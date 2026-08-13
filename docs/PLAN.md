---
PLAN: "feat: sitec — compilador de sitio con responsabilidad única"
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 4779389867153322203
---

> Este plan se despacha con el flujo CodeJob. Ver skill: agents-workflow.
>
> **Este repo se llamaba `github.com/tinywasm/ssr`.** Fue renombrado en GitHub,
> conservando los 151 commits de historia; GitHub redirige la ruta antigua. El
> cambio de `module path` es con ruptura y sin retrocompatibilidad. El plan
> maestro que coordina los demás repos está en
> https://github.com/tinywasm/app/blob/main/docs/PLAN.md

# Plan — `sitec`: el compilador de sitio

## Qué es

`sitec` toma un árbol de fuentes Go y produce la superficie estática desplegable
del sitio: hoja de estilos, bundle de scripts, sprite SVG, declaración de fuentes
y shell HTML. Corre hasta terminar y sale. Es un **compilador**, no un servidor
ni un renderizador.

El nombre viene de la convención del ecosistema para compiladores: `ormc`,
`ddlc`, `sitec`. El nombre anterior, `ssr`, nombraba una técnica que la librería
**no** implementa — nada se renderiza server-side por petición: los productores
corren una vez en build y el resultado es estático.

## Estado actual del repo — leer antes de empezar

El renombre ya está aplicado y **la librería compila**. Los archivos se
renombraron con `git mv`, así que `git blame` y `git log --follow` siguen
funcionando sobre todo el pipeline.

| Archivo | Antes | Contenido |
|---|---|---|
| `pipeline.go` | `ssr.go` | tipo `Extractor`, `ExtractAll`, `ExtractModule` |
| `invoke.go` | *(igual)* | selección de paquetes **y** ejecución del extractor — mezclados |
| `merge.go` | `extract.go` | merge por ruta, conflicto `@layer`, unicidad de `Fonts()` |
| `scanner.go` | *(igual)* | parseo AST, detección de productores |
| `cache.go` | *(igual)* | caché por hash de contenido |
| `assets.go` | **nuevo** | el DTO `Assets` |
| `tests/` | *(igual)* | 14 archivos de test |
| `emit_*.go` | **movidos desde `assetmin`** | ver abajo — **no compilan todavía** |
| `serve/serve.go` | `assetmin/http.go` | exposición en modo dev |

### El código de `emit` YA ESTÁ AQUÍ — no lo reescribas

Se movió desde `assetmin` código real y probado. **Adáptalo; no lo escribas de
nuevo.**

| Archivo aquí | Venía de | Qué contiene |
|---|---|---|
| `emit_asset.go` | `asset.go` | el artefacto: ruta, URL, mediatype, slots open/middle/close, caché minificada |
| `emit_html.go` | `html.go` | ensamblado del shell `index.html` |
| `emit_svg.go` | `svg.go` | recolección de sprites por módulo |
| `emit_fonts.go` | `fonts.go` | copia de las fuentes declaradas |
| `emit_filewrite.go` | `filewrite.go` | escritura a disco |
| `emit_injection.go` | `injection.go` | inyección de contenido |
| `emit_ssr_register.go` | `ssr_register.go` | ruteo de assets a slots |
| `emit_ssr_register_slot_test.go` | idem | su test |
| `emit_core.go` | `assetmin.go` | tipo núcleo, `Config`, y el registro de `tdewolff/minify` (3 líneas) |
| `emit_events.go` | `events.go` | `UpdateFileContentInMemory`, `processAsset` |
| `emit_inspect.go` | `inspect.go` | consultas sobre los artefactos (`ContainsCSS`, `HasIcon`…) |
| `emit_flush.go` | `ssr.go` | `FlushToDisk`, modo SSR |
| `emit_route.go` | mitad de `ssr_loader.go` | `routeAssets`, `resolveAndApplyRootCSS`, `isRootDir` |
| `serve/serve.go` | `http.go` | registro de rutas en modo dev |
| `tests/emit_*.go` | `assetmin/tests/` | **24 tests** del pipeline y `emit` |
| `tests/serve_*.go` | idem | 3 tests de exposición HTTP |
| `docs/ASSETMIN_*.md` | `assetmin/docs/` | 6 documentos del pipeline |

**Ya aplicado en el movimiento:**

- `package assetmin` → `package sitec` (y `package serve` en el subpaquete).
- `SSRAssets` → `Assets`.
- `emit_svg.go` ya usa `sprite.MergeAll` en vez de reimplementar la
  deduplicación, y `checkIconID` ya usa `sprite.Has` en vez de buscar
  `id="…"` por substring en el markup renderizado.

**`github.com/tinywasm/assetmin` está archivado y borrado.** No lo busques, no
lo reimportes, no restaures nada de él: todo lo que valía está en la tabla de
arriba, en `github.com/tinywasm/svg/sprite`, o en `tinywasm/app`.

**El trabajo pendiente de esta etapa:**

1. **Disolver `AssetMin`.** Es el objeto-dios de `assetmin`; llegó en
   `emit_core.go` porque los métodos movidos cuelgan de él. Deben colgar del tipo
   del pipeline de este repo. `emit_core.go` es material de referencia, no código
   final.
2. **`Config`** — conservar solo lo que `emit` usa (directorio de salida, nombres
   de archivo). El `Config` de `assetmin` servía también a HTTP y al watcher.
3. **La minificación se queda aquí, no se inyecta.** `emit_core.go` trae las tres
   líneas que la componen:
   ```go
   c.min = minify.New()
   c.min.AddFunc("text/css", css.Minify)
   c.min.AddFuncRegexp(...javascript..., js.Minify)
   c.min.AddFunc("image/svg+xml", minifySvg.Minify)
   ```
   El registro está indexado por los mediatypes que **este** repo emite: si
   añades un artefacto, registras su minificador. Mismo disparador de cambio, así
   que no hay interfaz que inventar — `sitec` depende de `tdewolff/minify`
   directamente, que es lo que ya hacía de forma transitiva.
4. Sustituir la escritura directa a disco de `emit_filewrite.go` por el puerto
   `FS` (ver 7.1).
5. **Repartir `emit_events.go`**: `NewFileEvent`, `ShouldCompileToWasm` y
   `MainInputFileRelativePath` son la interfaz de `devwatch` y se fueron a `app`;
   aquí solo se quedan `UpdateFileContentInMemory` y `processAsset`. El archivo
   llegó entero — recorta lo que no es de aquí.

`docs/ARCHITECTURE.md`, `docs/DESIGN.md`, `docs/SPECS.md`,
`docs/CONSTRUCTION_HARNESS.md` y `docs/diagrams/EXTRACTION.md` describen este
pipeline y siguen siendo válidos, pero **hablan de "SSR"**. Actualizarlos al
vocabulario de compilador es parte de la etapa 1.

### Lo que ya está hecho

**El DTO ya no vive en el consumidor.** `Assets` (antes
`assetmin.SSRAssets`) está en `assets.go`. La librería ya **no importa
`assetmin`**, así que ya no arrastra `router` (HTTP) ni `tui` (terminal) solo
para nombrar su propio resultado. Verificado: `grep -rn assetmin *.go` solo
encuentra el comentario histórico en `assets.go`.

### Lo que está roto, y por qué está bien

```
$ go build ./...           # 0 errores
$ go test ./tests/
vet: tests/consumer_hot_reload_test.go:37:21: cannot use extractor
     (variable of type *sitec.Extractor) as assetmin.SSRExtractor value:
       have ExtractAll() ([]*sitec.Assets, error)
       want ExtractAll() ([]*assetmin.SSRAssets, error)
FAIL github.com/tinywasm/sitec/tests [build failed]
```

Dos tests de integración (`consumer_hot_reload_test.go`,
`extract_root_wins_over_framework_test.go`) cablean el extractor dentro de
`assetmin` usando la interfaz vieja. **Esa rotura es el cambio con ruptura hecho
visible**, no un accidente: es exactamente la dependencia invertida que
eliminamos. La etapa 1 los repara.

Los otros 12 archivos de test son los del pipeline puro y son la red de
seguridad de todo este plan. Tres de ellos fallan **a propósito** — son la
reproducción del defecto y el criterio de aceptación de la etapa 2:

```
--- FAIL: TestExtractAll_UnreachablePackageDoesNotKillExtraction
--- FAIL: TestExtractAll_ReportsFailureInsteadOfSilentlyReturningNothing
--- FAIL: TestExtractAll_NoAssetLibrariesWarnedOnce
```

**No modifiques esos tres tests.** Haz que pasen.

### Dónde van los tests

**Todos en `tests/`, ninguno fuera.** Para agrupar se usa un prefijo en el
nombre (`emit_`, `serve_`, `extract_`), nunca una subcarpeta: fragmentar el
paquete obliga a duplicar helpers y hace que `go test ./tests/` deje de cubrir
todo. Un solo paquete, `sitec_test`.

La regla completa está en [AGENTS.md](../AGENTS.md) y es verificable:

```sh
find . -name "*_test.go" -not -path "./tests/*" -not -path "./.git/*"
# debe devolver vacío
```

---

## Etapa 1 *(gate)* — reparar los dos tests de integración

Los dos tests cablean `sitec.Extractor` dentro de `assetmin.AssetMin` para
verificar el recorrido completo hasta la hoja servida. Ese recorrido sigue
siendo válido y valioso; lo que cambió es el tipo del contrato.

`assetmin` debe declarar su puerto en términos del DTO de `sitec`:

```go
// en assetmin
import "github.com/tinywasm/sitec"

type SSRExtractor interface {
    ExtractModule(moduleDir string) (*sitec.Assets, error)
    ExtractAll() ([]*sitec.Assets, error)
}
```

Ahora la flecha apunta del consumidor al productor, que es la dirección
correcta. `assetmin.SSRAssets` se **elimina**; no se deja alias de
compatibilidad — es un cambio con ruptura declarado.

`assetmin` está archivado, así que no hay repo consumidor que actualizar: los
dos tests llegaron aquí junto con el código que cablean. Adáptalos al tipo del
pipeline de este repo.

### 1.1 Actualizar el vocabulario en la documentación

Los documentos de `docs/` heredados hablan de "SSR extractor". Renombrar a
compilador: `docs/ARCHITECTURE.md`, `docs/DESIGN.md`, `docs/SPECS.md`,
`docs/CONSTRUCTION_HARNESS.md`, `docs/diagrams/EXTRACTION.md` y `README.md`.

Actualizar también la descripción del repo en GitHub, que sigue diciendo *"SSR
asset extractor for TinyWasm..."*.

**Aceptación:** `go test ./tests/` compila; fallan solo los tres tests de
reproducción. `grep -rni "server-side render" docs/` devuelve vacío.

---

## Etapa 2 — extraer solo lo que se usa

Este es el defecto que dejaba la aplicación sin estilos. El principio:

```
El compilador ejecuta los productores de los paquetes que el artefacto
realmente importa. Nada más se recorre, importa, compila ni fusiona.
```

Medido desde `tinywasm/layout/platformd`:

| | paquetes de `tinywasm/components` |
|---|---|
| en el grafo del servidor | 7 |
| en el grafo del cliente WASM | 8 |
| **unión — lo que de verdad se usa** | **9** |
| lo que `expandToSSRPackages` importa hoy | **13** |

Uno de esos cuatro sobrantes, `calendarslider`, declara `RenderCSS()` e importa
`github.com/tinywasm/date`, que no está en el `go.sum` del consumidor porque
nadie importa calendarslider. El `main.go` generado lo importa igual, `go run`
no compila, y como ese único `go run` produce los assets de **todos** los
módulos, se pierden todas las hojas de estilo a la vez:

```
GET /style.css  ->  HTTP 200, 0 bytes
```

El desarrollador no puede arreglarlo a mano: `go mod tidy` no añade `date`
(nada importa el paquete que lo necesita), y el `go get` que sugiere el error
añade un requisito que el siguiente `tidy` borra.

### 2.1 El alcance se ancla en el directorio de arranque

El arnés se arranca desde la raíz de lo que se prueba, y **ese directorio no
suele ser la raíz del módulo**:

```
$ cd components/calendarslider && go list -m -json
Path: github.com/tinywasm/components
Dir:  /home/cesar/Dev/Project/tinywasm/components    <- raíz del módulo, NO calendarslider
Main: true
```

`components/` tiene un solo `go.mod`, así que probar un componente hoy recorre
el repo entero y extrae el CSS de los trece.

`Extractor.rootDir` ya guarda el directorio de arranque. Úsalo como ancla. **No**
subas a la raíz del módulo para calcular el alcance.

### 2.2 Calcular el conjunto alcanzable

**Archivo nuevo: `reach.go`.**

```go
// reachSet es el conjunto de rutas de importación en el grafo de compilación
// del directorio de arranque, en todas las configuraciones para las que se
// compila el artefacto.
type reachSet map[string]bool

// GraphLister devuelve las rutas de importación transitivas de pattern para el
// GOOS/GOARCH dado. Se inyecta para que los tests no necesiten toolchain.
type GraphLister func(rootDir, pattern, goos, goarch string) ([]string, error)
```

Implementación por defecto `goListDeps`:

- Comando: `go list -e -deps <pattern>`, `cmd.Dir = rootDir`, pattern `./...`
- `-e` es **obligatorio**: con el `GOOS` nativo el directorio del cliente es
  solo-WASM y reporta `build constraints exclude all Go files`; sin `-e` se
  aborta el listado completo.
- Entorno: heredar `os.Environ()` y añadir `GOOS=`/`GOARCH=` cuando no estén
  vacíos.
- La salida es una ruta por línea; ignorar líneas vacías.

**La unión de los dos grafos es obligatoria.** Una aplicación se compila dos
veces: servidor (`GOOS` nativo) y cliente WASM. Un componente que solo importa
el cliente no está en el grafo del servidor.

```go
// buildTargets son las configuraciones para las que se compila el artefacto.
// El conjunto alcanzable es su UNIÓN: un componente que solo importa el
// cliente WASM no aparece en el grafo del servidor, y descartarlo perdería sus
// estilos.
var buildTargets = []struct{ GOOS, GOARCH string }{
	{"", ""},       // nativo: el binario del servidor
	{"js", "wasm"}, // el cliente del navegador
}
```

Filtrar por un solo `GOOS` **pierde CSS en silencio** — `fieldset` y
`themetoggle` están solo en el grafo del cliente.

**Restricción de compatibilidad hacia adelante:** declara `GraphLister` como
tipo de función **inyectado** con setter exportado. La etapa 5 extrae el puerto
`Toolchain` y debe poder sustituir la implementación sin tocar ningún call site.
No llames a `exec.Command` desde la lógica del filtro.

### 2.3 Aplicar el filtro

En `invoke.go`, `modulesToAliases` empieza con:

```go
for _, m := range expandToSSRPackages(modules, scanner, assetLibraries) {
```

Filtrar ese slice contra el `reachSet`: conservar un paquete solo si su ruta de
importación está en el conjunto.

Loguear los descartes como **una sola línea agregada** por extracción — una
línea por paquete reintroduciría el ruido que este plan elimina:

```go
const skippedUnreachableFmt = "sitec: %d paquete(s) fuera del grafo de compilación fueron omitidos"
```

### 2.4 Degradar abierto, nunca cerrado

Si el `GraphLister` no puede producir un conjunto usable (sin toolchain, todos
los targets fallaron), **no filtrar**. Loguear una vez y seguir con la lista sin
filtrar, para que un sondeo roto degrade al comportamiento actual en vez de
emitir una hoja vacía en silencio.

Representarlo explícitamente — "vacío" y "desconocido" no pueden ser el mismo
valor:

```go
type reachability struct {
	set   reachSet
	known bool // false => no filtrar
}
```

### 2.5 Actualizar el test de módulo dependencia

`tests/extract_dependency_module_test.go` afirma hoy que el CSS del subpaquete
de un módulo dependencia se extrae **aunque el `main.go` de la app no lo
importe**. Esa afirmación codifica exactamente el comportamiento que estamos
eliminando.

Editar el fixture para que la app importe el paquete cuyo CSS espera:

```go
write(filepath.Join(appDir, "main.go"),
	"package main\n\nimport _ \"example.com/layout/platformd\"\n\nfunc main() {}\n")
```

La afirmación en sí no cambia y sigue siendo válida: el CSS de un módulo
dependencia vive en sus subpaquetes y debe recogerse. **No debilites el filtro
para conservar el fixture viejo.**

**Aceptación:**

- Pasa `TestExtractAll_UnreachablePackageDoesNotKillExtraction`.
- Desde `layout/platformd` se importan 9 paquetes de `components`, no 13, y cero
  líneas de `missing go.sum entry`.
- Desde `components/calendarslider` se extrae solo el grafo de ese componente.

---

## Etapa 3 — red de seguridad: paquetes que jamás se pueden importar

### 3.1 Un directorio `package main` nunca es un paquete del compilador

Toda aplicación tiene su entry point de cliente (`platformd/web/client.go`), y
las librerías de componentes publican demos dentro del módulo
(`components/calendarslider/web/`, `selectsearch/web/`, `themetoggle/web/`).
Todos son `package main` con `//go:build wasm`. **Un `package main` no se puede
importar desde ningún sitio.**

En `scanner.go`, `fileFeatures` gana un campo:

```go
type fileFeatures struct {
	mtime     time.Time
	pkgName   string          // f.Name.Name del archivo parseado
	imports   map[string]bool
	producers []producerDecl
}
```

`scanFile` ya tiene el `*ast.File` parseado; asignar `pkgName: f.Name.Name`.
`packageFeatures` gana el mismo campo, rellenado por `scanPackage` desde el
primer archivo no-test que lee.

En `expandToSSRPackages`, justo después de `scanner.scanPackage(path)`:

```go
// Un package main no se puede importar, así que nunca puede aportar assets.
if feats.PkgName == mainPackageName {
	return nil // no seleccionar; seguir bajando a subdirectorios
}
```

con `const mainPackageName = "main"`.

Devolver `nil`, **no** `filepath.SkipDir` — un directorio `package main` puede
contener subdirectorios con paquetes reales.

#### NO condicionar esto al módulo ni al nombre del directorio

Ambas alternativas fueron consideradas y rechazadas:

- *"saltar `web/` en módulos dependencia"* — cuando el arnés arranca en
  `components/calendarslider`, el módulo main es `components` y **las tres demos
  están en el módulo main**. Una exención por módulo main deja ese flujo sin
  protección.
- *"saltar directorios llamados `web` o `example`"* — depende de una convención
  de nombres que las librerías pueden ignorar.

La cláusula `package` es el hecho; todo lo demás es un sustituto de ella.

### 3.2 El scanner no evalúa build tags

`scanner.scanFile` parsea con `go/parser` en modo `0`, que lee el archivo sin
importar su línea `//go:build`. Un archivo solo-WASM que declare un productor es
detectado e importado en el extractor `!wasm`, donde no compila.

**No** intentes evaluar build constraints en el scanner. La etapa 2 deja esos
paquetes fuera de alcance y la 3.1 elimina la fuente común. Añade un comentario
en `scanFile` dejándolo escrito, para que el siguiente lector no "arregle" el
modo del parser.

**Aceptación:** un fixture con `<mod>/demo/client.go` que contiene `package
main` nunca se selecciona, ni en el módulo main ni en una dependencia.
`grep -rn '"web"' .` y `grep -rn '"example"' .` no devuelven nada en la lógica
de selección.

---

## Etapa 4 — una extracción, un error

### 4.1 `ExtractAll` invoca el extractor una sola vez

`ExtractAll` recorre los módulos llamando a `extractAssetsForModule`, que cada
vez calcula la misma clave de hash sobre el mismo conjunto y, al fallar, repite
el mismo `go run` fallido — la caché solo se escribe en éxito. Un fallo raíz se
convierte en N invocaciones del compilador y N líneas idénticas de log.

Reestructurar:

1. Resolver el `map[string]CollectorOutput` compartido **una vez** antes del
   bucle (consulta a caché, si no `invokeSSRExtractorOnce`).
2. Si falla, devolver `nil, err` de inmediato — **no** loguear por módulo ni
   hacer `continue`.
3. El bucle solo llama a `MergeResultsFor(m.path, results)` y fija
   `IsRoot`/`IsFramework`.

Extraer el paso compartido para que `ExtractModule` y `ExtractAll` usen un solo
camino:

```go
func (e *Extractor) results(rootDir string, modules []module) (map[string]CollectorOutput, error)
```

Borrar la línea de error por módulo:

```go
e.log("ssr extract error:", m.path, err)   // BORRAR
```

`grep -rn "extract error" .` debe devolver cero coincidencias en código que no
sea de test.

### 4.2 Un resultado vacío es un fallo

Si el bucle termina con `len(all) == 0`, devolver error en vez de `(nil, nil)`:

```go
const errNoAssetsExtracted = "sitec: ningún módulo produjo assets; la hoja de estilos saldría vacía"
```

Devolver `(nil, nil)` es lo que permitía al consumidor reportar éxito mientras
servía una hoja de 0 bytes.

### 4.3 Un paquete alcanzable que no compila debe reventar fuerte

La etapa 2 quita los paquetes fuera de alcance. Un paquete que **sí** está en el
grafo y no compila es código propio del desarrollador: debe romper el build con
su error de compilación real. No añadas ningún camino que se trague errores de
paquetes alcanzables.

**Aceptación:** pasa
`TestExtractAll_ReportsFailureInsteadOfSilentlyReturningNothing`; un error de
compilación real en un `css.go` propio produce **una** línea de error y un error
no-nil desde `ExtractAll`.

---

## Etapa 5 — avisar una sola vez sobre las asset libraries

El aviso `no asset libraries configured` se emite al inicio de `ExtractAll` y de
`ExtractModule`, o sea en cada arranque y en cada guardado. Observado diez veces
en un solo arranque.

Añadir `warnOnce sync.Once` al `Extractor` y envolver ambos sitios de log.
`SetAssetLibraries` debe asignar un `sync.Once` nuevo para que un llamador que
configure librerías más tarde siga comportándose bien en la siguiente
extracción.

**No** borres el aviso ni la API `SetAssetLibraries` — `tinywasm/app` empieza a
llamarla.

**Aceptación:** pasa `TestExtractAll_NoAssetLibrariesWarnedOnce`.

---

## Etapa 6 — el puerto `Toolchain`

Hoy `exec.Command("go", …)` está disperso por el ecosistema: `devflow` 7 sitios,
este repo 1 (más los que añade la etapa 2), `modfind` 1. Nadie posee "cómo este
proyecto ejecuta Go", así que el cacheo, el manejo de `GOOS` y la clasificación
de errores se reimplementan en cada sitio, distinto cada vez.

**Archivo nuevo `toolchain.go`:**

```go
// Toolchain ejecuta el toolchain de Go. Todo go list / go run / go build del
// compilador pasa por este puerto, para que el cacheo, el manejo de GOOS y la
// clasificación de errores existan en exactamente un lugar.
type Toolchain interface {
	List(dir string, args ...string) ([]byte, error)
	ListEnv(dir string, env []string, args ...string) ([]byte, error)
	Run(dir string, args ...string) ([]byte, error)
}
```

**Archivo nuevo `toolchain_exec.go`** con el adaptador real, y un fake en memoria
en los tests. Migrar los call sites de este repo y de `modfind`.

**Fuera de alcance, explícito:** los 7 sitios de `devflow` son comandos de flujo
de desarrollo (`gopush`, `gorelease`) y **no** se migran. No los toques.

**Aceptación:** `grep -rn 'exec.Command("go"' .` devuelve únicamente el
adaptador.

---

## Etapa 7 — separar las cuatro etapas del pipeline

`invoke.go` mezcla hoy selección y ejecución. Separar en archivos por etapa:

| Archivo | Contenido | Puerto |
|---|---|---|
| `select.go` | alcance, descarte de `package main`, detección de productores | `Toolchain` |
| `extract.go` | generación del `main.go` extractor, ejecución, parseo del JSON | `Toolchain` |
| `merge.go` | merge por ruta, conflicto `@layer`, sprite, unicidad de `Fonts()` | **ninguno — puro** |
| `emit.go` | minificación, shell HTML, escritura | `FS` |
| `pipeline.go` | composición de las cuatro; única API pública | — |

Que `merge.go` sea **puro** es el punto: sus tests corren sin toolchain, sin
filesystem y sin servidor.

`emit.go` se porta desde `assetmin` (`filewrite.go`, `html.go`, `injection.go`,
`fonts.go`, `svg.go`, `ssr_register.go`) más la mitad de compilación de
`ssr_loader.go` (`routeAssets`, `resolveAndApplyRootCSS`, `isRootDir`). **No
re-implementar desde cero:** portar y sustituir las llamadas directas por el
puerto.

`assetmin` no desaparece: `emit.go` lo **usa** como librería de minificación.
Esa es su única responsabilidad después de esta cadena.

### 7.1 El puerto `FS` con dos adaptadores — en memoria por defecto

**Requisito de producto, no negociable:** probar un componente no debe dejar
archivos en disco. Hoy `components/*/web/public` no existe en ninguno de los
trece componentes, y `layout/platformd/web/` contiene solo `client.go`. En modo
servidor interno **no se escribe nada**, ni siquiera el `.wasm` (`UseDiskStorage()`
se llama únicamente en la rama de disco). Eso se conserva.

El problema real de hoy no son los dos destinos: son **dos ensamblados
independientes** que hay que reconciliar a mano. La costura correcta es el
puerto `FS`:

```
emit ──escribe a través del puerto FS──┬── osFS   → web/public/ (producción, sitec build)
                                       └── memFS  → en memoria  (desarrollo, por defecto)
```

**Un solo `emit`, dos adaptadores.** `emit.go` nunca llama a `os.WriteFile`
directamente.

```go
// FS es el sumidero de la etapa emit. memFS no toca disco; osFS escribe.
type FS interface {
	Write(path string, content []byte, mediatype string) error
	Read(path string) ([]byte, string, bool)
	List() []Artifact
}

// Artifact se autodescribe: la ruta ES la URL. No hay una segunda tabla de
// rutas que mantener en sincronía.
type Artifact struct {
	Path      string
	Mediatype string
	Content   []byte
}
```

### 7.2 El subpaquete `serve` enumera, no declara

**Paquete nuevo `serve/`**, portado desde `assetmin/http.go` — pero **sin** su
tabla de rutas escrita a mano:

```go
// package serve
func RegisterRoutes(r router.Router, fs sitec.FS)   // recorre fs.List()
```

`assetmin/http.go` registra hoy cuatro handlers nombrados a mano
(`indexHtmlHandler`, `mainStyleCssHandler`, `mainJsHandler`,
`faviconSvgHandler`) más los standalone. Esa lista es una **segunda fuente de
verdad** sobre qué artefactos existen, y es exactamente lo que se desincroniza.
Sustituirla por un recorrido de `fs.List()`.

Conservar el matiz de `Cache-Control` que ya existe: `no-store` para texto
mutable en desarrollo, `immutable` en producción.

Va en un **subpaquete** para que `cmd/sitec` no arrastre `tinywasm/router`: el
CLI usa `osFS` y sale, no sirve nada.

**Por qué `serve` está aquí y no en `httpd`:** `serve` entrega los artefactos
de *este* compilador; `httpd.PublicDir` es un contrato genérico sobre un
directorio y sigue sirviendo el caso de disco sin cambios. Ninguno duplica al
otro: uno lee del sink en memoria, el otro del sistema de archivos.

### 7.3 Escritura atómica en `osFS`

`osFS` escribe a un temporal en el mismo directorio y renombra. Un fallo a mitad
debe dejar el archivo anterior intacto, nunca uno truncado — un CSS a medias es
peor que uno viejo.

**Aceptación:**

- `merge.go` no importa `os`, `os/exec` ni ningún puerto.
- `cmd/sitec` no importa `tinywasm/router`.
- `emit.go` no llama a `os.WriteFile`; solo al puerto `FS`.
- `serve` no contiene ninguna ruta literal: sale toda de `fs.List()`.
- **Tras `cd components/calendarslider && tinywasm -tui`, `git status` en
  `tinywasm/components` está limpio.** Esta es la comprobación de que el modo
  memoria sigue sin tocar disco; si falla, la regresión es invisible de otro
  modo.

---

## Etapa 8 — el CLI `cmd/sitec`

Contrato de ejecución. Lo van a manejar un runner de CI y un LLM, así que es
obligatorio:

- **Sin argumentos → ayuda por stdout, exit `0`.** Nunca bloquear en stdin ni en
  una TUI.
- `sitec build [-o dir]` → corre el pipeline, escribe la salida, exit `0`/`1`.
- `sitec check` → corre `select` + `extract` + `merge` y reporta, **sin escribir
  nada**. Es la puerta de CI que habría cazado el fallo de calendarslider antes
  de desplegar.
- **stdout = solo datos** (manifiesto JSON de lo producido); **stderr = todos los
  logs y diagnósticos**.
- Exit `0` en éxito y en ayuda; distinto de cero ante flags inválidos o fallo del
  pipeline.
- Sin watcher, sin navegador, sin demonio, sin red.

`cmd/sitec/main.go` contiene **solo**: parseo de flags, inyección de
dependencias, e imprimir/salir.

```go
// ❌ prohibido: lógica dentro de cmd/
func isProjectValid() bool { ... }

// ✅ correcto: exportado en la librería
func ValidateProject(dir string) error
```

**Aceptación:** un job de CI que corre `sitec check` falla ante un defecto de la
clase calendarslider y pasa en caso contrario, sin TUI y sin driver de base de
datos en su grafo de dependencias.

---

## Restricciones

### Este repo es herramienta de backend

`sitec` corre en la máquina del desarrollador y en CI, y maneja el toolchain de
Go. Usa legítimamente la biblioteca estándar: `os`, `os/exec`, `encoding/json`,
`go/ast`, `go/parser`, `sync`, `io`. La regla del ecosistema de "nada de
biblioteca estándar en código WASM" **no aplica aquí** — no "arregles" esos
imports. Para errores, usar `github.com/tinywasm/fmt` (`fmt.Err`).

### Sin strings hardcodeados

Todo string repetido es una constante con nombre. Las que este plan exige:
`skippedUnreachableFmt`, `mainPackageName`, `errNoAssetsExtracted`,
`buildTargets`.

### Sin carpetas `internal/`

En este ecosistema una carpeta `internal/` señala un fork o duplicación de una
dependencia en vez de contribuir aguas arriba. No crear ninguna.

### Cambio con ruptura, sin retrocompatibilidad

No dejes alias, ni tipos deprecados, ni caminos de fallback hacia la API vieja de
`ssr`. Si algo debe romperse, que rompa en tiempo de compilación y quede
registrado en el plan del repo afectado.

### Deuda heredada del movimiento

El template del `main.go` generado (en `invoke.go`) declara `type ssr struct`.
Es el tipo del **programa generado**, no de este paquete. Renómbralo a `assets`
al separar `extract.go` en la etapa 7.

### No hacer

- No llamar a `go get` ni `go mod tidy` desde este repo. Reparar el `go.mod` de
  un consumidor no es tarea del compilador, y para el caso de calendarslider está
  demostrado que no funciona.
- No añadir llamadas a `gopush` ni a `codejob`.
- No dejar un camino de "recorrer todo" en paralelo al filtro por alcance. El
  único fallback es el caso explícito de `known == false`.

---

## Etapas

| # | Alcance | Archivos | Aceptación |
|---|---|---|---|
| 1 *(gate)* | Reparar los 2 tests de integración; `assetmin` apunta a `sitec.Assets` | `tests/consumer_hot_reload_test.go`, `tests/extract_root_wins_over_framework_test.go` | `go test ./tests/` compila |
| 2 | Extraer solo el grafo alcanzable, anclado en el dir de arranque, unión nativo+WASM | `reach.go` (nuevo), `invoke.go`, `pipeline.go`, `tests/extract_dependency_module_test.go` | 9 de 13 paquetes desde `layout/platformd` |
| 3 | `package main` nunca es paquete; el scanner registra la cláusula | `scanner.go`, `invoke.go` | demos nunca seleccionadas en ningún módulo |
| 4 | Una extracción, un error; vacío = fallo | `pipeline.go`, `merge.go` | `grep -rn "extract error"` vacío fuera de tests |
| 5 | Avisar una vez sobre asset libraries | `pipeline.go` | pasa `TestExtractAll_NoAssetLibrariesWarnedOnce` |
| 6 | Puerto `Toolchain` + adaptador | `toolchain.go`, `toolchain_exec.go` | `exec.Command("go"` solo en el adaptador |
| 7 | Separar las cuatro etapas; portar `emit` desde `assetmin`; subpaquete `serve/` | `select.go`, `extract.go`, `merge.go`, `emit.go`, `pipeline.go`, `serve/` | `merge.go` sin `os`/`os/exec`; `cmd/sitec` sin `router` |
| 8 | CLI headless | `cmd/sitec/main.go` | `sitec check` falla ante calendarslider; sin args → ayuda, exit 0 |

Puerta final:

```
go test ./...
```

en verde, más una corrida real de `sitec build` y `sitec check` sobre
`tinywasm/layout/platformd` y sobre `veltylabs/mjosefa-cms`.

## En cola tras este plan

[PLAN_HTML_EN_APP.md](PLAN_HTML_EN_APP.md) — el HTML de los módulos debe caer
DENTRO de `#app`. Vivía en `tinywasm/assetmin`, pero los archivos que toca
(`html.go` y `routeAssets`) son política de compilación y llegan aquí en la
etapa 7. Ejecutarlo después de esa etapa.
