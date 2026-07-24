# PLAN_ASSETMIN: Endurecimiento del ensamblado de style.css en tinywasm/assetmin

Fecha: 2026-07-24
Estado: propuesto — el repo `tinywasm/assetmin` NO es accesible en esta sesión;
este documento queda aquí para trasladarlo cuando se trabaje en ese repo.

Contexto: la causa raíz principal del renderizado inconsistente se corrige en
`tinywasm/ssr` (ver `docs/PLAN.md`). Los puntos siguientes son defectos
latentes de `assetmin` v0.4.15 detectados durante esa investigación, que
conviene endurecer para que el ensamblado de `style.css` sea robusto ante
cualquier extractor.

## 1. `routeAssets` no reconcilia clave/slot entre carga inicial y reload

Archivo: `ssr_loader.go` → `routeAssets` + `ReloadSSRModule`.

- La carga inicial (`ScheduleSSRLoad` → `ExtractAll`) registra cada módulo con
  clave `a.ModuleName` y slot según `IsRoot` (`close` para el root, `middle`
  para el resto).
- `ReloadSSRModule(moduleDir)` registra lo que devuelva
  `ExtractModule(moduleDir)` con la clave/slot que el extractor decida.
- Si el extractor devuelve una clave o un `IsRoot` distinto para el mismo
  contenido (como hacía `ssr` antes del fix), `UpdateContentInSlot` **no
  encuentra la entrada previa y apila un duplicado en otro slot**. La regla
  vieja puede quedar después de la nueva y ganar la cascada CSS.

Propuesta: al registrar un módulo, si su `ModuleName` (o un nombre con el que
mantenga relación prefijo/contenido) ya existe en OTRO slot, reemplazar/retirar
la entrada previa en lugar de apilar. Alternativa mínima: indexar las entradas
CSS por módulo en los tres slots y hacer el `write` una operación
upsert-global, no por slot.

Reproducción disponible (desde el repo ssr):
`tests/consumer_hot_reload_test.go` → `TestConsumerHotReload_SubpackageEdit`.

## 2. Orden de inserción dependiente del timing en el arranque

`ScheduleSSRLoad` corre en goroutine; los eventos del watcher
(`ReloadSSRModule`) y `RegisterComponents` corren en paralelo. La posición de
cada módulo dentro de `contentMiddle` es su **orden de llegada**, así que dos
arranques de la misma app pueden producir `style.css` con orden distinto
(especialmente en workspaces con varios módulos locales). Esto se percibe como
"aleatoriedad" aunque no haya ningún mapa involucrado.

Propuesta: hacer el orden independiente de la llegada —
- insertar cada `ContentFile` en posición ordenada por `Path` dentro del slot
  (los `Path` son import paths de módulo → orden estable y reproducible), o
- mantener un índice de orden canónico (el de `ExtractAll`) y reordenar el slot
  al aplicar la carga inicial.

Reproducción disponible (desde el repo ssr):
`tests/consumer_hot_reload_test.go` → `TestConsumerStartup_ReloadRacingInitialLoad`.

## 3. Doble registro por claves heterogéneas: `RegisterComponents` vs extractor

`RegisterComponents` usa `fmt.Sprintf("%T", provider)` como clave (p. ej.
`*home.Home`), mientras el extractor usa import paths (`example.com/app`). Un
consumidor que use ambos caminos para el mismo componente obtiene el CSS
duplicado bajo dos claves que nunca se reconcilian.

Propuesta: documentar que son caminos excluyentes, o derivar la clave de
`RegisterComponents` del package path del tipo (`reflect.TypeOf(p).Elem().PkgPath()`)
para que coincida con la del extractor.

## 4. Fallo permanente de `ExtractAll` deja la app sin estilos en silencio

`ScheduleSSRLoad` reintenta 5 veces con backoff y, si todo falla (típico: la
app del usuario no compila justo en el arranque), loguea `FATAL` y **no
reintenta nunca más** — la app queda sin CSS hasta reiniciar, otro camino al
mismo síntoma "reinicio obligatorio".

Propuesta: marcar el estado "carga inicial pendiente" y reintentar la carga
completa en el siguiente evento del watcher (cuando el código vuelva a
compilar), en lugar de rendirse hasta el reinicio.

## 5. Menor: `FlushToDisk` itera `allAssets` (mapa)

Solo afecta el ORDEN en que se escriben archivos de salida distintos, no el
contenido de cada uno. Inocuo para el bug investigado; ordenarlo (`sort` por
`outputPath`) solo aporta logs/escrituras reproducibles.

## Verificación sugerida en assetmin

Portar los dos tests de integración del repo ssr
(`tests/consumer_hot_reload_test.go`) como tests propios de assetmin usando un
`SSRExtractor` fake que devuelva claves/slots inconsistentes a propósito: el
ensamblador debe producir el mismo `style.css` final en cualquier orden de
llegada de eventos.
