---
PLAN: "feat!: sitec.Build — un solo ensamblador y una salida que no puede ser parcial"
TAG: v0.1.0
---

> Este plan se despacha con el flujo CodeJob. Ver skill: agents-workflow.
>
> Es la **etapa B de una ola de 4** y es un **gate**: `tinywasm/app` y los
> sitios no pueden empezar hasta que esto esté publicado. Requiere que la
> **etapa A (`tinywasm/svg`)** ya esté publicada.
>
> Este plan es autocontenido: no necesitas leer nada fuera de este repositorio.

# Plan — `Build`/`WriteTo`: el pipeline se escribe una vez y su salida es un valor

## 1. Por qué

Hoy el pipeline de construcción de un sitio está escrito **tres veces**, casi
idéntico:

- `cmd/sitec/main.go` (este repo)
- `tools/build/main.go` de **cada sitio** (~90 líneas copiadas)
- `section-build.go` de `tinywasm/app`, con su propia variante

Los ocho pasos son siempre los mismos: `ValidateProject` → `New` → configurar
logger/finder/wasm builder → `ExtractAll` → `NewAssetMin` → `RouteExtractedAssets`
→ `LoadImages` → `FlushToDisk`. El harness
(`app-releases/docs/CONSTRUCTION_HARNESS.md`) es explícito: *"The glue is
written once, in the library that owns it. If every application would write the
same wiring, that wiring belongs to a piece — not to the applications."*

Tres copias derivan, y ya derivaron. La variante de `app` llama a
`FlushToDisk()` cuando en memoria solo está el módulo raíz —el escaneo completo
corre después, en una goroutine— y **vuelca a disco un sitio incompleto**. En
`veltylabs/mjosefa-website` eso convirtió un `style.css` de 21.467 bytes con
`@layer` en uno de 10.965 sin ninguna regla de componente. La página seguía
cargando con 200 en todo y sin errores de JavaScript.

La causa de fondo es que **la salida es un efecto secundario, no un valor**:
`FlushToDisk() error` escribe archivos y ya. No existe ningún tipo que
signifique "un sitio completo", así que "incompleto" es un estado
representable, y emitirlo no es un error sino un archivo más chico. El harness
lo cubre en el principio 3 (*illegal states unrepresentable*) y el 6 (*fail at
compile time … never silent failure*).

## 2. Contexto del repo para un agente sin contexto previo

- Módulo: `github.com/tinywasm/sitec`. `docs/PLAN.md` va junto a `go.mod`.
- **Este repo es herramienta de backend/build: usa librería estándar con toda
  normalidad.** La regla del ecosistema "nada de stdlib" aplica a los paquetes
  que compilan a WASM; aquí NO. No "arregles" imports de `os`, `path/filepath`,
  `encoding/json` ni `text/template`.
- Piezas que ya existen y que este plan **usa sin reescribir**:
  - `func New(rootDir string) *Extractor` — con `SetLog`, `SetFinder`,
    `SetAssetLibraries`, `SetWasmBuilder`, `WasmBuilder()`, `Finder()`,
    `ExtractAll() ([]*Assets, error)`.
  - `func NewAssetMin(ac *Config) *AssetMin` — con `SetLog`, `SetFS`,
    `SetSSRExtractor`, `SetImageProcessor`, `SetWasm`, `Write`,
    `RouteExtractedAssets([]*Assets) error`, `FlushToDisk() error`,
    `List() []Artifact`, `Read(path)`.
  - `type Config struct { OutputDir, RootDir, AppName, AssetsURLPrefix string; DevMode bool; SiteURL string }`
  - `type FS interface { Write(path string, content []byte, mediatype string) error; Read(path string) ([]byte, string, bool); List() []Artifact }`
    y `func NewOsFS() FS`.
  - `type Artifact struct { Path, Mediatype string; Content []byte }`
  - `func NewDefaultWasmBuilder(debug bool) WasmBuilder`
  - `func ValidateProject(dir string) error`
  - El procesado de imágenes vive en `github.com/tinywasm/image/min`:
    `min.New(&min.Config{RootDir, OutputDir, Quality})`, con `SetLog`,
    `SetFinder`, `LoadImages() error`.
- Prohibidas las cadenas repetidas en la lógica: todo literal repetido va a una
  constante con nombre exportada (`const DefaultOutputDir = "web/public"`, …).
- `cmd/` fino: solo parseo de flags, inyección y print/exit. Toda decisión vive
  en la librería y es exportada y testeable.

## 3. La API a construir

Archivo nuevo: **`build.go`**.

```go
// Mode decide qué artefacto produce Build. Es un tipo, no un bool, porque los
// dos modos difieren en más de una cosa (compilador de WASM, minificado,
// directorio de salida por defecto) y un bool no dice cuál es cuál en la
// llamada.
type Mode uint8

const (
	// ModeRelease es el entregable: WASM por TinyGo, minificado.
	ModeRelease Mode = iota
	// ModeDev es la caché de desarrollo: compilación rápida, sin minificar.
	ModeDev
)

// BuildConfig son las decisiones del sitio. Todo lo demás lo decide el modo.
type BuildConfig struct {
	RootDir string // raíz del módulo (donde está go.mod). Obligatorio.
	Mode    Mode

	// OutputDir es relativo a RootDir. Vacío => DefaultOutputDir en
	// ModeRelease, DefaultDevOutputDir en ModeDev.
	OutputDir string

	SiteURL string // habilita sitemap.xml y URLs canónicas absolutas
	AppName string

	// StaticAssets son rutas relativas a RootDir que se copian verbatim a la
	// salida, conservando su ruta relativa. Es el hueco por el que cada sitio
	// tenía que escribir su propio tools/build: el pipeline de imágenes solo
	// emite variantes de los image.Asset declarados, así que un logo SVG o un
	// PNG de marca no llegaban nunca.
	StaticAssets []string

	ImageQuality int // 0 => DefaultImageQuality

	// AssetLibraries son las librerías de estilo cuyos importadores deben
	// declarar un productor. Vacío => sin comprobación.
	AssetLibraries []string

	Log func(...any) // nil => silencio
}

// Site es el resultado de una construcción COMPLETA. No hay forma de obtener
// uno parcial: Build devuelve error antes que un Site al que le falte algo.
type Site struct { /* campos no exportados */ }

// Build ejecuta el pipeline entero en memoria. No toca el disco de salida.
func Build(cfg BuildConfig) (*Site, error)

// Artifacts devuelve todo lo producido, listo para inspeccionar en un test.
func (s *Site) Artifacts() []Artifact

// WriteTo vuelca el sitio. Acto explícito y separado de construirlo.
func (s *Site) WriteTo(fs FS) error
```

Constantes exportadas que este plan introduce (nada de literales sueltos):

```go
const (
	DefaultOutputDir    = "web/public"
	DefaultDevOutputDir = ".tinywasm/public"
	DefaultImageQuality = 82
)
```

### La invariante que hay que hacer imposible de romper

`Build` devuelve `(*Site, error)`. Debe devolver **error, nunca un `Site`**,
cuando:

- `ValidateProject(cfg.RootDir)` falla;
- `ExtractAll()` falla;
- `ExtractAll()` devuelve **cero módulos** — hoy `app` trata ese caso como
  `errEmptySSRExtraction` en su propio bucle de reintentos; la comprobación
  pertenece aquí. Mensaje verbatim:
  `"sitec: extracción vacía: ningún módulo aportó assets"`;
- `RouteExtractedAssets` falla;
- la construcción de WASM falla (cuando corresponde compilarla).

`Site` no tiene constructor exportado ni campos exportados. La única forma de
obtener uno es que `Build` haya terminado los ocho pasos.

## 4. Pasos

### Paso 1 — `build.go`

Crear el archivo con los tipos y constantes de la sección 3, y con `Build`
haciendo, **en este orden**:

1. Validar `cfg.RootDir` no vacío y absoluto; si es relativo, resolverlo con
   `filepath.Abs`. (Ya hubo un defecto por pasar `"."`: la comparación de
   pertenencia de módulos usa rutas absolutas de `go list -m` y fallaba en
   silencio.)
2. `ValidateProject(root)`.
3. `e := New(root)`, `e.SetLog(cfg.Log)`, `e.SetAssetLibraries(cfg.AssetLibraries)`.
4. Si existe `filepath.Join(root, "web", "client.go")`, `e.SetWasmBuilder(NewDefaultWasmBuilder(cfg.Mode == ModeDev))`.
5. `all, err := e.ExtractAll()`; error si falla o si `len(all) == 0`.
6. `am := NewAssetMin(&Config{OutputDir: <resuelto>, RootDir: root, AppName: cfg.AppName, SiteURL: cfg.SiteURL, DevMode: cfg.Mode == ModeDev})`, `am.SetLog(cfg.Log)`.
7. Procesador de imágenes: `min.New(...)` con `SetFinder(e.Finder())` y `SetLog`; `am.SetImageProcessor(...)`.
8. Si hay wasm builder: construir, `am.SetWasm(...)`, `am.Write(...)`.
9. `am.RouteExtractedAssets(all)`.
10. `imgHandler.LoadImages()`.
11. Copiar `cfg.StaticAssets` (paso 2).
12. Devolver `&Site{...}` con el `*AssetMin` dentro.

`WriteTo(fs FS)` recorre `s.Artifacts()` y llama a `fs.Write`. `FlushToDisk`
sigue existiendo para `AssetMin`; `WriteTo` es la vía pública nueva y la que
usan `cmd/` y los consumidores.

### Paso 2 — activos estáticos declarados

Implementar `StaticAssets`: para cada entrada, copiar de
`filepath.Join(root, entry)` a la misma ruta relativa dentro de la salida. Si
una entrada no existe, **error**, no silencio:
`"sitec: activo estático declarado y ausente: %s"`.

Acepta tanto archivo suelto como directorio (copia recursiva).

### Paso 3 — `cmd/sitec/main.go` adelgaza

Reescribirlo para que sea **solo** flags + `Build` + `WriteTo` + exit. Todo
`if` de decisión que quede ahí hoy se va a `build.go`. Sin `ExtractAll`, sin
`NewAssetMin`, sin `LoadImages`, sin `FlushToDisk` en `cmd/`.

Criterio: `grep -n "ExtractAll\|NewAssetMin\|LoadImages\|FlushToDisk" cmd/sitec/main.go` → vacío.

### Paso 4 — migrar los dos usos de `Sprite.Merge`

La etapa A borró el método. Estas dos líneas dejan de compilar y hay que
cambiarlas a la función:

- `merge.go` (~línea 66): `merged.Icons = merged.Icons.Merge(out.Icons)` →
  `merged.Icons = sprite.MergeAll(merged.Icons, out.Icons)`
- `extract.go` (~línea 118): `mergedSprite = mergedSprite.Merge(sp)` →
  `mergedSprite = sprite.MergeAll(mergedSprite, sp)`

Subir la dependencia a la versión publicada por la etapa A.

### Paso 5 — el test con forma de consumidor (lo más importante del plan)

El harness: *"An API is not published until a consumer-shaped test, inside the
library itself, proves it."* Este test es la razón de ser de la etapa. Archivo
nuevo: **`tests/build_consumer_test.go`**.

Montar en `t.TempDir()` un proyecto de fixture real:

- `go.mod` de un módulo con dos paquetes.
- Paquete raíz con `RenderPages()` que devuelva una página cuyo body use el
  componente y contenga `<svg><use href="#fixture-icon"/></svg>`.
- Paquete `component/` con, en archivos con `//go:build !wasm`:
  - `css.go`: `RenderCSS() *css.Stylesheet` que emita un selector reconocible,
    p. ej. `.fixture-widget`.
  - `svg.go`: `IconSvg() *sprite.Sprite` que declare el símbolo `fixture-icon`.

Luego:

```go
site, err := sitec.Build(sitec.BuildConfig{RootDir: dir, Mode: sitec.ModeRelease})
// err debe ser nil
```

Y afirmar sobre `site.Artifacts()`:

1. El CSS emitido **contiene** `.fixture-widget`. Falla hoy si el CSS de las
   dependencias se pierde — que es exactamente el defecto de producción.
2. El CSS emitido **contiene** `@layer` — sin capas no aplica ninguna regla de
   componente.
3. La página emitida **contiene** `id="fixture-icon"`. Falla hoy si el sprite
   se emite vacío (el defecto que dejó sin iconos a todo el ecosistema).

Este único test cubre las tres formas en que el pipeline ha fallado en
silencio. Si es incómodo de escribir, la API es incómoda de usar: ese es el
defecto a arreglar, no el test a simplificar.

### Paso 6 — test de la invariante

En el mismo archivo: un fixture cuyo `ExtractAll` no produzca módulos (un
directorio con `go.mod` y nada más). Afirmar que `Build` devuelve **error** y
`nil` como `*Site`, no un sitio vacío.

## 5. Criterios de aceptación

1. `grep -n "ExtractAll\|NewAssetMin\|LoadImages\|FlushToDisk" cmd/sitec/main.go` → vacío.
2. `grep -rn "\.Merge(" .` → vacío.
3. Existe `func Build(cfg BuildConfig) (*Site, error)` y `func (s *Site) WriteTo(fs FS) error`.
4. `Site` no tiene campos exportados ni constructor exportado distinto de `Build`.
5. `go build ./... && go vet ./... && go test ./...` en verde.
6. Los tres asertos del paso 5 existen y pasan.

## 6. Qué NO hacer

- **No** borres `FlushToDisk`, `RouteExtractedAssets`, `ExtractAll` ni
  `NewAssetMin`: `tinywasm/app` sigue usándolos hasta la etapa C. Esta etapa
  **añade** el camino único; retirar el viejo es trabajo posterior.
- **No** toques `tinywasm/app` ni ningún sitio desde este repo.
- **No** cambies el formato de la salida (nombres de archivo, minificado,
  capas). Este plan mueve el ensamblado, no el resultado: el test del paso 5
  debe pasar contra el mismo CSS que hoy produce `cmd/sitec`.
- **No** purgues la librería estándar: este repo es tooling de backend y la usa
  legítimamente.

## 7. Etapas

| # | Archivo | Qué |
|---|---|---|
| 1 | `build.go` (nuevo) | `Mode`, `BuildConfig`, `Site`, `Build`, `WriteTo`, constantes |
| 2 | `build.go` | Copia de `StaticAssets` con error si falta |
| 3 | `cmd/sitec/main.go` | Adelgazar a flags + `Build` + `WriteTo` |
| 4 | `merge.go`, `extract.go`, `go.mod` | `sprite.MergeAll` y subir `tinywasm/svg` |
| 5 | `tests/build_consumer_test.go` (nuevo) | Test con forma de consumidor |
| 6 | `tests/build_consumer_test.go` | Invariante: extracción vacía ⇒ error |
