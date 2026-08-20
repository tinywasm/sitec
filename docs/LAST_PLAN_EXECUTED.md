---
PLAN: "feat: el proyecto declara qué clase de sitio es — RenderSite()"
EXECUTOR: jules
REVIEWER: none
---

> Plan autocontenido: todo lo necesario para ejecutarlo está aquí.
> Se despacha con el flujo CodeJob. Ver skill: agents-workflow.
> Reglas del repo: `AGENTS.md` en la raíz — léelo antes de tocar nada
> (los tests van TODOS en `tests/`).

# Plan — `RenderSite()`: que el proyecto declare, y dejar de obligarle a
# escribirse su propio build

## 1. El problema, con el caso real que lo destapó

`acme/acme-web` es un sitio estático construido
con `sitec`. Para poder publicarlo tuvo que escribirse **su propio driver de
build**, `tools/build/main.go`, de 130 líneas, que hace exactamente lo mismo que
`sitec build` **más dos cosas que `sitec` no le deja declarar**:

1. **`SiteURL`** — sin él, `sitec` no emite `sitemap.xml` (`emit_core.go`: "Emit
   sitemap.xml if SiteURL is set"). El campo existe en `Config`, pero el CLI
   `sitec build` (`cmd/sitec/main.go:71`) no lo pasa y el proyecto no tiene
   dónde ponerlo.
2. **Los activos estáticos** — los SVG de marca (`logo-completo.svg`,
   `logo-movil.svg`, `escudo-marca.png`). `RenderImages()` solo cubre
   rásters, que procesa `image/min`. El proyecto acabó copiándolos con una
   función a mano, `copyBrandPictures`.

`BuildConfig.StaticAssets []string` **ya existe** (`build.go:34`) y hace justo
eso… pero no hay forma de llegar a él: ni el CLI lo expone ni el proyecto puede
declararlo.

Consecuencia medida en desarrollo:

```
404 GET http://localhost:8080/img/logo-completo.svg
```

El logo de la marca no existe en el entorno de desarrollo, porque el demonio no
corre `tools/build`. El proyecto tiene **dos** definiciones distintas de sí
mismo y solo una se usa en cada contexto.

Esto es literalmente el caso descrito en `docs/CONSTRUCTION_HARNESS.md`: *"un
hueco de API siempre aflora en la hoja, donde el agente no tiene autoridad para
publicar aguas arriba — así que lo parchea localmente"*. Se cierra aquí, en la
librería que es dueña del concepto.

## 2. Lo que se construye

Un productor nuevo, hermano de `RenderCSS` / `RenderImages` / `RenderPages`, que
el módulo **raíz** declara para describir su sitio:

```go
//go:build !wasm

package site

import "github.com/tinywasm/sitec"

func RenderSite() sitec.Site {
	return sitec.Site{
		URL: "https://acme.example",
		StaticAssets: []string{
			"img/logo-completo.svg",
			"img/logo-movil.svg",
			"img/escudo-marca.png",
		},
	}
}
```

Declararla tiene un segundo significado, y es el importante:

> **Un módulo raíz que declara `RenderSite()` es un sitio estático.**
> Su entregable es el directorio de salida y su `index.html` lo manda
> `RenderPages()`. Sin `RenderSite()`, el proyecto es una aplicación y el
> `index.html` es el shell (`<div id="app">` + `main.js`).

Hoy esa diferencia se adivina por accidente: `AssetMin` construye **siempre** el
shell (`emit_core.go:310`) y luego lo **sustituye** si alguien declara páginas
(`emit_route.go:103`). Quién gana depende de en qué momento se pregunte.

## 3. El tipo

En `build.go`, junto a `BuildConfig`:

```go
// Site es lo que un módulo RAÍZ declara sobre el sitio que produce.
//
// Declararla convierte al proyecto en un sitio estático: el entregable es el
// directorio de salida y RenderPages() es el dueño del index.html. Un proyecto
// sin RenderSite() es una aplicación y su index.html es el shell de arranque
// del WASM.
type Site struct {
	// URL es la URL pública del sitio. Habilita sitemap.xml y las URL
	// canónicas absolutas. Vacía ⇒ no se emite sitemap.
	URL string

	// StaticAssets son rutas relativas a la raíz del módulo que se copian
	// verbatim a la salida. Para lo que NO pasa por el pipeline de imágenes:
	// SVG de marca, PDF, robots.txt.
	//
	// Un archivo o directorio declarado y ausente es un ERROR de build, no un
	// aviso: un logo que falta en producción se descubre demasiado tarde.
	StaticAssets []string
}
```

`BuildConfig.SiteURL` y `BuildConfig.StaticAssets` **se quedan** (son la vía del
llamador programático, y `Build` los sigue respetando). Regla de precedencia,
documentada en el doc comment de `BuildConfig`:

> Lo que declara `RenderSite()` **manda** sobre lo que traiga `BuildConfig`.
> El proyecto es la autoridad sobre sí mismo; `BuildConfig` es el afinado del
> llamador. Cuando ambos traen valor y difieren, se registra un aviso con los
> dos valores y se aplica el del proyecto.

## 4. Etapas

### Etapa 1 · Extracción

`extract.go` genera un `main.go` temporal que importa cada módulo y serializa lo
que declaran (`CollectorOutput`). Hay que añadir el nuevo productor:

- `receiverFeature`: campo nuevo `HasSite bool`.
- El escáner detecta `func RenderSite() sitec.Site` igual que detecta
  `RenderPages` (mismo mecanismo, mismo archivo).
- `CollectorOutput`: campo nuevo `Site *Site` (puntero: distinguir "no
  declarada" de "declarada vacía" es el corazón de este plan).
- `Assets`: campo nuevo `Site *Site`, propagado desde `CollectorOutput`.

**El archivo fuente convencional es `site.go`.** Añádelo a la lista de archivos
de assets que dispara recarga en caliente si el repo la tiene; `tinywasm/app`
tiene la suya (`ssr_watcher.go`, `ssrTextAssetFiles`) y su plan la actualiza.

### Etapa 2 · Solo el raíz puede declararla

En `RouteExtractedAssets` (`emit_core.go:146`), con las mismas reglas que ya
aplican a `Fonts` y `RootCSS`:

```go
// mensajes exactos, como constantes con nombre
"sitec: el módulo %s declara RenderSite() pero no es el proyecto raíz — solo el raíz describe el sitio; se ignora"
"sitec: RenderSite() declarada por dos módulos raíz: %s y %s"
```

Un módulo no raíz que la declare: **aviso ruidoso e ignorada**, nunca silencio.

### Etapa 3 · Aplicar `URL`

Donde hoy se lee `c.SiteURL` (emisión de `sitemap.xml` y resolución de
canónicas en `emit_route.go:emitPages`), el valor efectivo pasa a ser:
`Site.URL` si el raíz la declaró; si no, `Config.SiteURL`.

Resuélvelo **una sola vez**, al enrutar, en un campo ya existente — nada de un
`if` repetido en cada punto de uso.

### Etapa 4 · Aplicar `StaticAssets`

`copyStaticAssets` ya existe (`build.go:186`) y hace exactamente lo que hace
falta: `os.Stat` → error si falta, `filepath.Walk` para directorios, `am.Write`
con el mediatype detectado.

- Llamarla también con los activos que declara `RenderSite()`, unidos a los de
  `BuildConfig.StaticAssets` (sin duplicados).
- Que sea alcanzable desde el camino de `AssetMin` que usa el demonio, no solo
  desde `Build()`. Método exportado nuevo en `AssetMin`:

```go
// LoadStaticAssets copia a la salida los activos declarados por RenderSite().
// Separado de RouteExtractedAssets porque un activo estático no participa en la
// cascada de CSS ni en el sprite: solo se copia.
func (c *AssetMin) LoadStaticAssets() error
```

Mensaje de error exacto cuando falta un archivo declarado (ya existe, resérvalo
tal cual): `sitec: activo estático declarado y ausente:`

### Etapa 5 · El CLI deja de estar cojo

`cmd/sitec/main.go` no necesita banderas nuevas: `Build` ya lee `RenderSite()`
del proyecto. Verifica que `sitec build` en un proyecto con `RenderSite()`
emita `sitemap.xml` y copie los activos **sin pasar ni un flag**.

Mantén `cmd/` fino: cero lógica nueva ahí. Si hace falta decidir algo, se decide
en la librería.

### Etapa 6 · Diagnóstico del dueño de `index.html`

Hoy, si nadie declara páginas, la salida es el shell — y para un sitio estático
eso significa **publicar una página en blanco** sin un solo error.

Con `RenderSite()` la intención ya es explícita, así que se puede exigir:

```
sitec: el proyecto declara RenderSite() (es un sitio estático) pero ningún módulo declara RenderPages(): la salida sería el shell de una aplicación, no un sitio
```

Que sea **error de build**, no aviso. Regla nueva a documentar en
`docs/ARCHITECTURE.md`:

| El raíz declara | index.html lo manda | Entregable |
|---|---|---|
| `RenderSite()` + `RenderPages()` | las páginas SSR | el directorio de salida, versionable |
| ni una ni otra | el shell (`#app` + `main.js`) | lo arma quien despliega |
| `RenderSite()` sin `RenderPages()` | **error de build** | — |
| `RenderPages()` sin `RenderSite()` | las páginas SSR, sin sitemap ni activos estáticos | aviso: falta `RenderSite()` |

## 5. Tests — todos en `tests/`

| Archivo | Qué prueba |
|---|---|
| `tests/site_extract_test.go` | `RenderSite()` en el raíz llega a `Assets.Site` |
| `tests/site_solo_raiz_test.go` | declarada por un módulo no raíz → avisada e ignorada |
| `tests/site_url_test.go` | `Site.URL` emite `sitemap.xml` sin tocar `BuildConfig` |
| `tests/site_url_precedencia_test.go` | `Site.URL` gana a `BuildConfig.SiteURL` y avisa |
| `tests/site_static_assets_test.go` | los archivos declarados aparecen en la salida con su mediatype |
| `tests/site_static_ausente_test.go` | un activo declarado y ausente es error, no aviso |
| `tests/site_sin_pages_test.go` | `RenderSite()` sin `RenderPages()` → error de build |

**Y el test con forma de consumidor** (la regla que mantiene honesto el arnés —
`docs/CONSTRUCTION_HARNESS.md`): un proyecto de prueba completo en `tests/` con
`site.go`, `css.go`, `page.go` y un SVG de marca, del que se hace
`Build(ModeRelease)` y se comprueba que la salida contiene el SVG, el
`sitemap.xml` y el `index.html` con el markup de la página. Si escribir ese test
resulta incómodo, la API es incómoda: eso es el hallazgo, no el test.

## 6. Criterios de aceptación

| # | Comprobación | Esperado |
|---|---|---|
| 1 | `go test ./...` | verde |
| 2 | `find . -name "*_test.go" -not -path "./tests/*" -not -path "./.git/*"` | vacío |
| 3 | Un proyecto con `RenderSite()` + `sitec build` sin flags | emite `sitemap.xml` y copia los activos estáticos |
| 4 | `grep -rn "SiteURL" .` | sigue existiendo en `BuildConfig` (compatibilidad) |
| 5 | `RenderSite()` sin `RenderPages()` | error con el texto exacto de la etapa 6 |
| 6 | Activo estático ausente | error `sitec: activo estático declarado y ausente:` |

## 7. Defecto asociado: `PublishImages` escribe en disco siempre

Fuera del alcance principal de este plan, pero **hay que decidirlo aquí** porque
es de esta librería.

`emit_images.go` (v0.1.4) hace, para cada imagen procesada:

```go
urlKey   := path.Join("/", a.Path)                 // memoria — lo que sirve el servidor
diskPath := filepath.Join(outputDir, a.Path)       // disco
...
if err := fs.Write(diskPath, a.Content, a.Mediatype); err != nil {
	return err
}
```

y el FS por defecto de `NewAssetMin` es `NewOsFS()` (`emit_core.go:287`).

Consecuencia medida: el demonio de desarrollo reescribe
`<proyecto>/web/public/img/*.jpg` **en cada arranque** (mtimes `22:04` → `22:13`
→ `22:42` en un proyecto real). El contenido sale idéntico porque el pipeline es
determinista, así que `git status` queda limpio y nadie se entera — hasta que
cambie la calidad o el conjunto de variantes.

Eso contradice la regla ya fijada en
`tinywasm/docs/SINGLE_OUTPUT_MASTER_PLAN.md`:

> El demonio no escribe el sitio. Lo sirve desde memoria. Queda un solo
> directorio, `web/public`, con un solo productor: el build de release.

Dos arreglos, y hacen falta **los dos**:

1. **En `tinywasm/app`** (su plan, etapa 1): inyectar `sitec.NewMemFS()` en el
   `AssetMin` del demonio, para que **nada** de lo que haga `sitec` pueda tocar
   el proyecto. Es la barrera dura.
2. **Aquí**: `PublishImages` publica en el `FS`; escribir en disco es
   responsabilidad de quien decide volcar (`Site.WriteTo`, `FlushToDisk`), no
   un efecto secundario de publicar. Quita el `fs.Write` de `PublishImages`
   **si y solo si** compruebas que el camino de release (`Build` →
   `WriteTo`) sigue emitiendo las imágenes: `Build` usa `NewMemFS`, así que las
   imágenes deben estar en `Artifacts()` para llegar al disco. Añade un test que
   lo fije:

   `tests/site_release_incluye_imagenes_test.go` — `Build(ModeRelease)` +
   `WriteTo` produce los archivos de `img/` en la salida.

   Si al quitarlo el release pierde las imágenes, **no lo quites**: reporta que
   `Artifacts()` no las incluye y arréglalo primero.

## 8. Fuera de alcance (lo hacen otros planes)

- **Que el demonio deje de escribir en el proyecto** → `tinywasm/app`. Este plan
  no toca `PublishImages` ni `FlushToDisk`.
- **El orden de arranque del demonio** (servir antes de tener el sitio completo)
  → `tinywasm/app`.
- **Borrar `tools/build/main.go` de `acme/acme-web`** → ese repo, cuando esto
  se publique.
