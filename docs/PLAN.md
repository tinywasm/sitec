---
PLAN: "feat!: sitec emite sitios multipágina y procesa imágenes en build"
EXECUTOR: jules
REVIEWER: none
---

> Este plan se despacha con el flujo CodeJob. Ver skill: agents-workflow.

# Plan — `sitec` no puede compilar un sitio de contenido todavía

Dos huecos, ambos descubiertos preparando `veltylabs/mjosefa-website` (un sitio
de clínica con página por especialidad). Van juntos porque los dos rompen el
mismo artefacto: el `web/public/` que produce `sitec build`.

## Hueco 1 — `sitec build` nunca procesa imágenes

`cmd/sitec/main.go`'s `runBuild` —el camino de CI/CD, la razón de ser de este
CLI— construye e invoca el `WasmBuilder`, pero **no existe el equivalente para
imágenes**. Un proyecto con fotos declaradas en `image.go` obtiene un
`web/public/` con CSS y JS, y las imágenes sin optimizar o ausentes.

### Por qué pasa

`sitec` ya declara un puerto `ImageProcessor` (`emit_core.go:85`) con
`SetImageProcessor` (línea 107), **pero es deliberadamente angosto**: solo tiene
`UnobservedFiles() []string`, y su único uso es que el vigilante de ficheros
ignore las imágenes generadas (`emit_events.go:167`). `sitec` nunca llama a
`LoadImages()`.

Quien sí lo dispara es `tinywasm/app`, y **no** por ese puerto: guarda su propia
referencia concreta al handler (inyectada en `app/section-build.go:267` vía
`min.New(&min.Config{...})`) y la llama en su propio bucle de reintentos
(`app/ssr_loader.go:125`). El puerto alimenta la exclusión del vigilante;
`LoadImages()` es de quien construye el handler.

### El arreglo

**No cambies la interfaz `ImageProcessor`.** Replica en `runBuild` el patrón de
dos pasos que `app/section-build.go:267` ya usa: construir el handler, dárselo a
`AssetMin` para la exclusión, y llamar `LoadImages()` sobre la referencia
concreta.

```go
// cmd/sitec/main.go — en runBuild, junto al bloque del WasmBuilder
imgHandler := min.New(&min.Config{
	RootDir:   ".",
	OutputDir: filepath.Join(*outputDir, "img"),
	Quality:   82,
})
imgHandler.SetLog(func(msg ...any) { fmt.Fprintln(os.Stderr, msg...) })
am.SetImageProcessor(imgHandler)
// ... tras RouteExtractedAssets ...
if err := imgHandler.LoadImages(); err != nil { /* fatal */ }
```

**Antes de escribir una condición de activación**, comprueba cómo `min.Handler`
resuelve "no hay declaraciones" (`LoadImages`/`SetFinder` en
`tinywasm/image/min`). El WASM se activa mirando un fichero fijo
(`web/client.go`, línea 77) pero `image.go` puede vivir en **cualquier** paquete
del proyecto — el pipeline los descubre vía `tinywasm/modfind`. Si `LoadImages()`
ya devuelve sin error cuando no hay nada declarado, **no inventes una segunda
detección**: constrúyelo siempre.

Reusar el `modfind.Finder` que el `Extractor` ya construye evita una segunda
pasada de `go list -m` (`app` lo hace en `section-build.go:271`). Si exponerlo
cuesta más que el subproceso que ahorra, déjalo — es optimización, no el bug.

## Hueco 2 — solo se puede emitir UNA página

`emit_core.go:254-255` crea **un** `indexHtmlHandler` con `urlPath = "/"`
fijo. No hay concepto de rutas: cada módulo aporta fragmentos HTML a los slots
de ese único documento (`emit_ssr_register.go:147`).

Para un sitio de contenido eso es descalificante. El caso que lo motiva: una
clínica que quiere posicionar "oftalmología chillán", "gastroenterología
chillán", "laboratorio chillán" — consultas distintas, con intención distinta.
Una sola URL compite por todas con un solo `<title>`, una sola meta
description y un solo canonical. Y sin varias URLs no hay `sitemap.xml` que
tenga sentido, ni páginas citables por un motor generativo.

### El contrato ya está publicado

**No diseñes los tipos: ya existen** en `github.com/tinywasm/html v0.0.17`:

```go
// tinywasm/html/providers.go
type Page struct {
	Path string          // "/" o "/especialidades/oftalmologia/"
	Doc  DocumentOptions // Title, Description, Canonical, Image, JSONLD
	Body string
}

type PagesProvider interface {
	RenderPages() []Page
}
```

Viven en `html` y **no** en este repo a propósito: un productor no puede
importar a su consumidor para nombrar su propio resultado — es exactamente el
defecto que motivó desmontar `assetmin`. `DocumentOptions` ya emite
`description`, `canonical`, `og:*` y JSON-LD tipados.

### El arreglo

`RenderPages` es un **séptimo productor**, con el mismo camino que los seis
actuales. Los cuatro puntos a tocar, todos ya existentes:

| Paso | Fichero | Qué |
|---|---|---|
| descubrir | `scanner.go:93-100` | añadir `"RenderPages": true` al mapa `producerNames` |
| clasificar | `select.go:148-161` | `case "RenderPages": rf.HasPages = true` (+ el campo en `receiverFeature`) |
| invocar | `extract.go:185-212` | rama en la plantilla, **en las dos formas** (con `.Name`/instancia y sin ella) |
| transportar | `extract.go:15-22` | `Pages []html.Page` en `CollectorOutput`, y en `Assets` |

Luego, en `emit`: **una `Page` = un fichero**. `Path` terminado en `/` escribe
`<path>index.html` (para que la URL sirva sin extensión); si no, `<path>` tal
cual. El `Body` va dentro del shell de `html.DocumentString`, con
`CSSURL`/`JSURL`/`FaviconURL` rellenados por el compilador — es la única capa
que conoce los nombres construidos de los assets.

**La página raíz sigue funcionando igual.** `RenderHTML()` no se toca ni se
deprecia: un módulo que solo lo implementa sigue aportando al `index.html` de
siempre. `RenderPages()` es aditivo. Un `Path: "/"` en `RenderPages` sí colisiona
con el índice de `RenderHTML` — **eso es un error, no una fusión silenciosa**:
falla con un mensaje que nombre los dos módulos.

### `sitemap.xml`

Con N páginas, emitirlo es trivial y es la mitad del valor del cambio: un fichero
más en la salida, listando cada `Path` como URL absoluta. Necesita saber el
dominio, que `sitec` hoy no conoce — **añádelo como campo de `Config`**
(p. ej. `SiteURL string`). Si está vacío, **no emitas `sitemap.xml`** en vez de
inventar un dominio: un sitemap con URLs relativas o con `localhost` es peor que
ninguno. El mismo `SiteURL` es lo que permite resolver `Canonical` a absoluto
cuando el módulo lo declaró relativo.

## Restricciones

- Este repo es herramienta de backend: usa la biblioteca estándar
  legítimamente. No "arregles" esos imports.
- No cambies la firma de `ImageProcessor`. Si al investigar concluyes que hace
  falta ensancharlo, **dilo en el PR** en vez de hacerlo: es decisión de diseño.
- No toques `app/section-build.go` ni `app/ssr_loader.go` — están bien
  cableados y sirven de referencia, no de objetivo. `app` debe seguir compilando
  y pasando su suite **sin cambios** tras este plan.
- Todo string repetido es una constante con nombre.
- Sin carpetas `internal/`.
- El cambio es con ruptura solo si `Config` gana un campo obligatorio; procura
  que `SiteURL` vacío degrade a "sin sitemap", no a error.

## Verificación

- **Imágenes**: proyecto de prueba con `image.go` y una imagen real →
  `sitec build -o <tmp>` deja las variantes WebP en `<tmp>/img/`.
- **Multipágina**: un módulo de prueba con `RenderPages()` devolviendo dos
  páginas (`/` y `/x/y/`) → salida con `index.html` y `x/y/index.html`, cada uno
  con SU `<title>` y SU `<meta name='description'>`, distintos entre sí. Ésta es
  la aserción que importa: dos ficheros con el mismo `<title>` significan que la
  metadata no se está propagando por página.
- **Sitemap**: con `SiteURL` puesto, `sitemap.xml` lista ambas URLs absolutas;
  con `SiteURL` vacío, no existe el fichero y no hay error.
- **Regresión**: un proyecto que solo usa `RenderHTML()` produce exactamente el
  mismo `index.html` que antes de este plan.
- `go build ./... && go vet ./... && go test ./...` verde en este repo, y
  `go test ./...` verde en `tinywasm/app` sin tocarlo.

## Etapas

| # | Alcance | Archivos | Aceptación |
|---|---|---|---|
| 1 | Cablear `min.Handler` en `runBuild` | `cmd/sitec/main.go` | build real emite las variantes en `<out>/img/` |
| 2 | `RenderPages` como séptimo productor (descubrir → clasificar → invocar → transportar) | `scanner.go`, `select.go`, `extract.go` | un módulo con `RenderPages()` llega a `Assets.Pages` con Path/Doc/Body intactos |
| 3 | Emitir un fichero por página, con su shell y metadata | `emit_core.go`, `emit_ssr_register.go` | dos páginas → dos ficheros con títulos y descriptions distintos; el caso `RenderHTML`-solo no cambia |
| 4 | `Config.SiteURL` + `sitemap.xml` | `emit_core.go` | sitemap con URLs absolutas; ausente y sin error si `SiteURL` está vacío |
| 5 | Tests de los cuatro puntos de Verificación | `tests/` | suite verde aquí y en `app` |
