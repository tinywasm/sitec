# PLAN: Corrección de la extracción de assets CSS (renderizado inconsistente hasta reiniciar)

Fecha: 2026-07-24
Estado: propuesto — pendiente de revisión

## 1. Síntoma reportado

Al usar `tinywasm/ssr` desde una aplicación consumidora (vía `assetmin`), a veces
el renderizado difiere de lo esperado: los estilos de `style.css` no quedan en el
orden correcto y hay que **detener y reiniciar** la aplicación consumidora para
que los componentes se rendericen bien.

Hipótesis inicial: un `map` de Go (iteración aleatoria) en `tinywasm/ssr` o
`tinywasm/css` que baraja el orden del CSS extraído.

## 2. Investigación realizada

### 2.1 Hipótesis del mapa aleatorio: DESCARTADA en ssr

Test: `tests/deterministic_order_test.go` → `TestExtract_DeterministicAcrossRuns`.

Ejecuta la extracción completa (generación de `main.go` + `go run`) varias veces
con un `Extractor` nuevo en cada iteración (simula reinicios del proceso) y
compara la salida byte a byte. **Pasa de forma estable** (verificado con
`-count=5`): la extracción de ssr es determinista.

Motivo por código: todos los puntos donde un mapa podría filtrar orden están
protegidos —

| Punto | Mecanismo | Determinista |
|---|---|---|
| `MergeResultsFor` (extract.go) | `sort.Strings(paths)` antes de fusionar | ✅ |
| `expandToSSRPackages` (invoke.go) | `filepath.WalkDir` (orden léxico) | ✅ |
| Salida del `main.go` generado | `encoding/json` serializa mapas con claves ordenadas; además se parsea a mapa y se consume vía `MergeResultsFor` | ✅ |
| Descubrimiento de módulos (`modfind`) | `go list -m -json all` (orden estable de la toolchain) | ✅ |
| `computeModuleHashSet` (cache.go) | `sort.Strings(filePaths)` | ✅ |

Librerías vecinas revisadas desde la caché de módulos:

- `tinywasm/css@v0.1.4`: DSL 100 % basado en slices, **sin mapas** → determinista. No es la culpable.
- `tinywasm/modfind@v0.0.4`: parsea el stream de `go list` en orden → determinista.
- `tinywasm/svg@v0.1.8` (sprite): slices + dedupe; `assetmin` además ordena por nombre de módulo al renderizar el sprite → determinista.
- `tinywasm/assetmin@v0.4.15`: el contenido de `style.css` se ensambla desde slices por slot (`contentOpen/Middle/Close`), no desde mapas. El único `range` sobre mapa (`FlushToDisk` sobre `allAssets`) solo afecta el orden en que se escriben archivos distintos, no el contenido de cada uno.

### 2.2 Causa raíz REAL: discordancia de clave/slot entre carga inicial y hot-reload

Test que lo reproduce: `tests/consumer_hot_reload_test.go` (**falla hoy — es el
criterio de aceptación de este plan**). Reproduce el cableado exacto del
consumidor: `assetmin.SetSSRExtractor(ssr.New(root))` + `LoadSSRModules()` +
evento de watcher → `ReloadSSRModule(dir)`.

El flujo con bug:

1. **Arranque** — `assetmin.LoadSSRModules()` llama a `ssr.ExtractAll()`, que
   devuelve **una entrada fusionada por módulo Go** (todos los subpaquetes
   combinados por `MergeResultsFor`), con `ModuleName = path del módulo`
   (p. ej. `example.com/app`) e `IsRoot = true` para el módulo raíz.
   `assetmin.routeAssets` la registra con **clave `example.com/app` en el slot
   `close`**.

2. **Hot-reload** — el desarrollador edita `modules/beta/css.go`. El watcher de
   assetmin llama `ReloadSSRModule(<root>/modules/beta)`, que invoca
   `ssr.ExtractModule(<root>/modules/beta)`. Como ese dir no es un módulo,
   `ssr.go` **sintetiza un módulo para el subpaquete**: devuelve
   `ModuleName = example.com/app/modules/beta` e
   `IsRoot = isRootDir(moduleDir, rootDir) = false`. assetmin lo registra con
   **clave distinta (`.../modules/beta`) en un slot distinto (`middle`)**.

3. **Resultado** — la regla nueva no reemplaza a la vieja (clave y slot no
   coinciden), queda **duplicada** y, como `middle` se escribe **antes** que
   `close`, la regla **vieja queda después y gana la cascada CSS**:

   ```
   arranque:   :root{--brand:#00ADD8}.config{order:0}.alpha{order:1}.beta{color:blue}.zeta{order:3}
   tras editar: :root{--brand:#00ADD8}.beta{color:red}.config{order:0}.alpha{order:1}.beta{color:blue}.zeta{order:3}
                                       ^^^^ nueva (middle)                            ^^^^ vieja (close) — GANA
   ```

   El navegador sigue mostrando el estilo viejo hasta reiniciar la app. Es
   exactamente el síntoma reportado.

4. **Variante de arranque** — `TestConsumerStartup_ReloadRacingInitialLoad`
   demuestra que un evento de watcher que llega mientras `LoadSSRModules()`
   (asíncrono) aún corre produce un `style.css` distinto al de un arranque
   limpio (entrada duplicada / orden alterado). Como depende del *timing* de
   eventos, entre ejecuciones el resultado varía — lo que se percibía como
   "aleatoriedad de mapas".

**Conclusión de atribución**: el bug NO está en `tinywasm/css`. Es un contrato
inconsistente entre `ssr.ExtractAll` y `ssr.ExtractModule`, consumido por
`assetmin.routeAssets`. La corrección principal corresponde a **este repo
(ssr)**; hay endurecimiento complementario recomendado en `assetmin` (ver
`docs/PLAN_ASSETMIN.md`, repo no accesible en esta sesión).

## 3. Plan de corrección (en este repo)

### Paso 1 — `ExtractModule` debe resolver el módulo PROPIETARIO (fix principal)

Archivo: `ssr.go` (bloque de síntesis de subpaquete, líneas ~58-75).

Cambio de semántica: cuando `moduleDir` es un subdirectorio de un módulo
conocido, en lugar de sintetizar un módulo para el subpaquete, resolver al
**módulo que lo contiene** y devolver sus assets fusionados completos:

```go
// antes: target = {path: m.path + "/" + rel, dir: moduleDir}   (subpaquete)
// después: target = m                                          (módulo dueño)
```

Y calcular `IsRoot` con el dir del módulo resuelto, no con el dir editado:

```go
a.IsRoot = isRootDir(target.dir, e.rootDir)
```

Con esto, un reload tras editar `modules/beta/css.go` devuelve
`ModuleName = example.com/app`, `IsRoot = true` — **la misma clave y el mismo
slot** que registró la carga inicial → `UpdateContentInSlot` reemplaza la
entrada **in place** (misma posición, sin duplicados, orden estable).

Beneficios directos:
- El CSS editado reemplaza al viejo → no más reinicios.
- El reload es idempotente y conmutativo con la carga inicial → desaparece la
  variación entre arranques (test de carrera pasa).
- Funciona igual para módulos de dependencia con `replace` local
  (editar `layout/platformd/css.go` recarga `example.com/layout` completo).

Costo: el reload re-extrae el módulo completo en vez de un subpaquete. Es el
mismo `go run` de siempre (se compila el programa colector completo en ambos
casos); el cache por hash (`computeModuleHashSet`) ya amortigua repeticiones.

### Paso 2 — Actualizar el test de semántica antigua

`tests/extract_subpackage_test.go` (`TestExtractModule_Subpackage`) hoy fija la
semántica de subpaquete (`ModuleName = example.com/parent/sub`, CSS solo del
subpaquete). Debe actualizarse a la nueva semántica: extraer desde `sub/`
devuelve el módulo `example.com/parent` fusionado (que en ese fixture contiene
únicamente `.sub{color:red}`, así que el assert de contenido casi no cambia —
cambia el `ModuleName` esperado).

### Paso 3 — Verificación (criterios de aceptación)

Deben pasar TODOS:

1. `TestConsumerHotReload_SubpackageEdit` — hoy FALLA (regla vieja duplicada que gana la cascada).
2. `TestConsumerStartup_ReloadRacingInitialLoad` — hoy FALLA (salida distinta con evento en carrera).
3. `TestExtract_DeterministicAcrossRuns` — ya pasa; no debe romperse.
4. Suite existente (`extract_test.go`, `extract_module_root_test.go`, `extract_dependency_module_test.go`, `mergeicons_test.go`) — verde, con `extract_subpackage_test.go` adaptado (Paso 2).

Ejecutar además con `-count=3` para confirmar estabilidad entre procesos.

### Paso 4 (opcional, limpieza) — Eliminar código muerto

`scanner.go` (`newScanner`, `ScanProjectImports`, `moduleSubpackagesUsed`) no
tiene llamadores en el repo. `moduleSubpackagesUsed` itera un mapa sin ordenar
— si algún día se reactivara, reintroduciría justo la clase de bug investigada
aquí. Eliminarlo o dejarle un `sort` defensivo.

## 4. Fuera del alcance de este repo

Ver `docs/PLAN_ASSETMIN.md` para el endurecimiento complementario en
`tinywasm/assetmin` (repo no accesible en esta sesión). El fix del Paso 1 es
suficiente para eliminar el síntoma reportado, pero assetmin puede blindarse
para que futuros extractores inconsistentes no reproduzcan el problema.
