---
PLAN: "fix(serve): el sitio se resuelve en cada petición, no en una foto al registrar"
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 932892522728194436
---

> Este plan se despacha con el flujo CodeJob. Ver skill: agents-workflow.
> Es autocontenido: no necesitas leer nada fuera de este repositorio.

# Plan — servir el sitio vivo, no una foto congelada

## Contexto: qué hace este repo

`github.com/tinywasm/sitec` compila un sitio estático desde módulos Go. El
resultado vive en un `FS` (`assets.go`):

```go
type FS interface {
	Write(path string, content []byte, mediatype string) error
	Read(path string) ([]byte, string, bool)
	List() []Artifact
}

// Artifact se autodescribe: la ruta ES la URL.
type Artifact struct {
	Path      string
	Mediatype string
	Content   []byte
}
```

Hay dos implementaciones: `NewMemFS()` (memoria) y `NewOsFS()` (disco).
`AssetMin` es el emisor: acumula artefactos y los escribe con `FlushToDisk()`.

El subpaquete `serve/` expone el `FS` por HTTP a través de `tinywasm/router`.

**Este repo es herramienta de backend y usa la biblioteca estándar
legítimamente.** No "corrijas" imports a `tinywasm/fmt` por regla ecosistémica:
`sitec` nunca se compila a WASM.

## Los dos defectos

El consumidor es un servidor de desarrollo que sirve el sitio **desde memoria**.
Medido contra él, con el sitio ya completo en el `FS`:

```
/                       200 text/html  10858B   ← debería ser 12220
/style.css              200 text/css   10965B   ← debería ser 21467
/img/logo-completo.svg  200 text/html  10858B   ← cae al index en vez de 404
```

### Defecto 1 — el contenido queda congelado al registrar

`serve/serve.go` hace hoy esto:

```go
for _, art := range fs.List() {
	a := art
	r.PublicAsset(a.Path, func(ctx router.Context) {
		...
		ctx.Write(a.Content)   // ← contenido capturado en el closure
	})
}
```

`fs.List()` se recorre **una vez**, al registrar las rutas. El consumidor
registra las rutas al arrancar el servidor y **después** completa el sitio (el
escaneo de módulos de dependencias tarda segundos). Todo lo que llegue después
es invisible: el `style.css` servido es para siempre el del arranque, sin el
CSS de ninguna dependencia. Y un artefacto con una ruta nueva no tiene ruta
registrada en absoluto.

### Defecto 2 — un archivo ausente se disfraza de página

Una ruta desconocida devuelve `index.html` con 200. Un `/img/logo.svg` que no
existe se ve en el navegador como una imagen rota sin ninguna señal en el
servidor. Ese enmascaramiento ocultó durante semanas que un sitio no emitía sus
logos.

## Paso 1 — `serve.RegisterRoutes` resuelve en tiempo de petición

Reescribe `serve/serve.go` para registrar **una sola** ruta, no una por
artefacto.

`router.Router` expone `PublicAsset(path string, h router.HandlerFunc)`. El
adaptador HTTP lo registra en un `http.ServeMux`, donde el patrón `/` actúa
como comodín: cualquier ruta más específica registrada por otro componente
(por ejemplo `/client.wasm`) sigue ganando. `router.Context` expone
`Path() string` y `WriteStatus(code int)`.

Contrato exacto de la nueva `RegisterRoutes`:

```go
// RegisterRoutes expone el FS completo bajo una sola ruta comodín.
//
// Una ruta por artefacto no sirve: se registrarían en un instante y el sitio
// se completa después, así que el servidor quedaba sirviendo la foto del
// arranque —un style.css sin el CSS de ninguna dependencia— y las rutas
// nacidas más tarde no existían.
func RegisterRoutes(r router.Router, fs sitec.FS)
```

Resolución, en este orden:

1. `key := strings.TrimPrefix(ctx.Path(), "/")`
2. Si `key == ""` o termina en `"/"`, añade `indexFile`.
3. Si `key` es exactamente `spriteFile`, responde 404: el sprite se inyecta
   dentro del HTML y no se expone como recurso propio. (Hoy se excluye con el
   mismo criterio; conserva la exclusión.)
4. `content, mediatype, ok := fs.Read(key)`. Si `ok`, sirve.
5. Si no, y el último segmento de `key` **no** contiene un punto, reintenta con
   `key + "/" + indexFile`. Si esta vez `ok`, sirve.
6. Si no, responde 404 con `Content-Type: text/plain; charset=utf-8` y cuerpo
   exactamente `notFoundBody`.

Constantes nuevas en `serve/serve.go` — nada de literales repetidos:

```go
const (
	indexFile    = "index.html"
	spriteFile   = "icons.svg"
	notFoundBody = "404 no encontrado"
	mediatypeText = "text/plain; charset=utf-8"
)
```

Cabeceras al servir (conserva la lógica actual, que ya es correcta):

- si `mediatype` contiene `"text/"` →
  `Cache-Control: no-cache, no-store, must-revalidate`
- en caso contrario → `Cache-Control: public, max-age=31536000, immutable`

**Elimina** el bucle sobre `fs.List()` y la captura de `a.Content`.
Criterio: `grep -n "fs.List()" serve/serve.go` no devuelve nada.

> Consecuencia esperada: `router.Routes()` deja de enumerar una fila por
> activo. Es correcto — ahora hay una sola ruta. Si algún test existente afirma
> el número de rutas registradas o busca una ruta por nombre de archivo,
> reescríbelo para afirmar **los bytes servidos** por una petición HTTP, que es
> lo que importa. Los tests de `serve/` son
> `serve_http_test.go`, `serve_http_public_test.go` y el helper
> `serve_http_helper_test.go`.

## Paso 2 — las imágenes entran al `FS`

`AssetMin` conoce un procesador de imágenes a través de una interfaz en
`emit_core.go`:

```go
type ImageProcessor interface {
	UnobservedFiles() []string
}
```

`github.com/tinywasm/image/min` (ya en `go.mod` de este repo, y ya importado
por `build.go`) expone ahora:

```go
type Artifact struct {
	Path      string // relativa a la raíz del sitio, y ES la URL: "img/foto.M.jpg"
	Mediatype string
	Content   []byte
}

func (h *Handler) Artifacts() []Artifact
```

Amplía la interfaz:

```go
type ImageProcessor interface {
	UnobservedFiles() []string
	Artifacts() []min.Artifact
}
```

y añade a `AssetMin` un método nuevo, en el archivo nuevo `emit_images.go`:

```go
// PublishImages mete las imágenes ya procesadas en el FS, que es lo único que
// el servidor de desarrollo consulta.
//
// Sin esto una imagen solo existía como archivo en disco, así que la única
// manera de servirla era que el demonio creara un directorio de salida dentro
// del proyecto del usuario —una segunda salida del sitio, con bytes distintos
// a los del entregable de release.
//
// Es idempotente: reescribe cada ruta con el contenido actual.
func (c *AssetMin) PublishImages() error
```

Implementación: si `c.imageProcessor` es `nil`, devuelve `nil`. Si `c.fs` es
`nil`, devuelve `fmt.Err("sitec: PublishImages sin FS configurado")`. Para cada
artefacto llama a `c.fs.Write(a.Path, a.Content, a.Mediatype)` y propaga el
primer error. Toma el `c.mu` para leer `c.imageProcessor` y `c.fs`, y **suéltalo
antes** de escribir en el `FS` (el `FS` tiene su propio lock).

> `fmt` aquí es `github.com/tinywasm/fmt`, que este repo ya usa; si el archivo
> importa el `fmt` estándar, usa `fmt.Errorf` con el mismo texto. Resuelve cuál
> es por el bloque de imports, no por el texto del selector.

**No toques `Build()` ni `build.go`.** El build de release sigue escribiendo
las imágenes a disco por su camino actual, que está verificado byte a byte
contra sitios en producción. Este plan solo añade el camino de memoria.

## Paso 3 — tests con forma de consumidor

Una API no está publicada hasta que un test **con la forma del consumidor**,
dentro de esta librería, la demuestra. El consumidor real hace: registrar rutas
→ completar el sitio → pedir por HTTP.

En `serve/`, añade:

```go
// TestSirveElEstadoActualNoElDelRegistro reproduce el defecto exacto: las
// rutas se registran cuando el sitio aún está incompleto y el escaneo de
// dependencias aterriza después.
func TestSirveElEstadoActualNoElDelRegistro(t *testing.T)
```

1. `fs := sitec.NewMemFS()`; escribe `style.css` con `".a{color:red}"`.
2. Registra las rutas y levanta el servidor de prueba (usa el helper que ya
   existe en `serve_http_helper_test.go`).
3. **Después** de registrar, escribe `style.css` con
   `".a{color:red}.b{color:blue}"` y escribe una ruta que no existía al
   registrar: `especialidades/index.html`.
4. Afirma que `GET /style.css` devuelve el contenido **nuevo**, y que
   `GET /especialidades/` devuelve el HTML nuevo con 200.

```go
// TestUnArchivoAusenteEs404NoLaPortada: enmascarar un activo ausente con la
// portada convierte un fallo del build en una imagen rota sin señal.
func TestUnArchivoAusenteEs404NoLaPortada(t *testing.T)
```

Afirma que `GET /img/no-existe.svg` devuelve 404 y **no** el cuerpo de
`index.html`, con el `index.html` presente en el `FS`.

```go
// TestElSpriteNoSeExponeComoRecurso
func TestElSpriteNoSeExponeComoRecurso(t *testing.T)
```

Con `icons.svg` presente en el `FS`, `GET /icons.svg` devuelve 404.

En `tests/`, añade:

```go
// TestPublishImagesHaceServibleUnaImagen
func TestPublishImagesHaceServibleUnaImagen(t *testing.T)
```

Con un `ImageProcessor` de prueba (un stub local que devuelva un `min.Artifact`
fijo), afirma que tras `PublishImages()` el artefacto aparece en `fs.Read` con
su mediatype, y que `serve` lo devuelve por HTTP con `Content-Type` correcto.

## Reglas de código

- **Nada de literales repetidos**: `"index.html"`, `"icons.svg"`, el cuerpo del
  404 y el mediatype de texto son constantes con nombre, declaradas arriba.
- **No inventes API nueva** fuera de: `PublishImages`, las constantes de
  `serve/serve.go` y el método añadido a `ImageProcessor`.
- **No cambies `Artifact`, `FS`, `memFS`, `osFS`, `FlushToDisk` ni `Build`.**
- El idioma de comentarios sigue al del archivo que tocas; para archivos nuevos
  (`emit_images.go`) usa español, como en los bloques citados.

## Etapas

| # | archivo | entrega | criterio de aceptación |
|---|---|---|---|
| 1 | `serve/serve.go` | ruta única con resolución en tiempo de petición | `grep -n "fs.List()" serve/serve.go` vacío |
| 2 | `emit_core.go` | `ImageProcessor` gana `Artifacts()` | `go build ./...` |
| 3 | `emit_images.go` | `PublishImages` | `go build ./...` |
| 4 | `serve/`, `tests/` | los cuatro tests nuevos | `go test ./...` verde |

Cierre: `go vet ./...` y `go test -race ./...` en verde.
