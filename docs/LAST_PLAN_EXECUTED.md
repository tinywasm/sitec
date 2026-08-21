---
PLAN: "feat: un proyecto declara su icono y el shell emite el juego completo"
TAG: v0.2.0
EXECUTOR: jules
REVIEWER: none
---

> Plan autocontenido: todo lo necesario para ejecutarlo está aquí.
> Se despacha con el flujo CodeJob. Ver skill: agents-workflow.
> Reglas del repo: [`AGENTS.md`](../AGENTS.md) en la raíz — léelo antes de tocar
> nada (los tests van TODOS en `tests/`).

# Plan — el favicon: hoy se enlaza un archivo vacío

**Requisito previo**, porque este entorno no lo trae instalado:

```bash
go install github.com/tinywasm/devflow/cmd/gotest@latest
```

**Requisito previo de dependencia:** este plan usa
`github.com/tinywasm/image/favicon`, el paquete que deriva el juego de iconos de
un logo cuadrado. Empieza con:

```bash
go get github.com/tinywasm/image@latest
```

Si ese paquete todavía no existe en la versión publicada, **detente y dilo en el
PR**: este plan no se puede completar sin él y no hay que reimplementarlo aquí.

## 1. El problema, medido

`sitec` trata `favicon.svg` como un artefacto **suyo**: lo declara en
`emit_core.go` (`NewFaviconSvgHandler`), lo reescribe en cada build, y el shell
enlaza esa ruta desde `emit_html.go`:

```go
func (h *htmlHandler) generateFaviconLink() []byte {
	return []byte(`<link rel="icon" type="image/svg+xml" href="` + h.faviconURL + `">`)
}
```

Nadie llena ese archivo. La comprobación, hecha sobre un proyecto real
(`veltylabs/misitio`):

1. Escribir un `web/public/favicon.svg` válido a mano.
2. `goflare build`.
3. El archivo queda en **0 bytes**.

Resultado: **cada página que emite `sitec` enlaza un archivo vacío**. La pestaña
muestra el icono por defecto del navegador y el proyecto no tiene forma de
arreglarlo — los ocho nombres de productor que este repo reconoce (`RootCSS`,
`RenderCSS`, `RenderHTML`, `RenderJS`, `IconSvg`, `Fonts`, `RenderPages`,
`RenderSite`) **no incluyen ninguno para el icono**.

Un proyecto declara su CSS en Go y no puede declarar su icono. Eso es lo que este
plan corrige.

## 2. Qué se añade

Un **noveno productor**, `Favicon()`, con la misma mecánica que ya usa
`RenderSite()`: el recolector generado lee los campos del valor devuelto y los
serializa a JSON en su propia estructura *wire*, así que el proyecto puede
devolver un tipo de `tinywasm/image/favicon` sin que el recolector lo importe.

Lo que declara un proyecto, en su archivo `!wasm` de configuración:

```go
//go:embed logo.png
var logo []byte

func (b *Brand) Favicon() favicon.Source {
	return favicon.Source{Raster: logo}
}
```

### 2.1 — Reconocer el productor

- `scanner.go`: agregar `"Favicon": true` a la lista de nombres.
- `select.go`: `case "Favicon": rf.HasFavicon = true`.
- `extract.go`: `HasFavicon bool` en `receiverFeature`, y en `CollectorOutput`:

  ```go
  Favicon *faviconWire `json:"favicon"`

  type faviconWire struct {
      Raster []byte `json:"raster"`
      SVG    []byte `json:"svg"`
  }
  ```

  `encoding/json` codifica `[]byte` en base64 sin ayuda: no inventes un
  transporte.

- `extract.go`, en **las dos ramas** de la plantilla —la de receptor con nombre y
  la de paquete suelto—, junto al bloque de `HasSite`:

  ```go
  {{if .HasFavicon}}
  {
      f := inst.Favicon()
      s.Favicon = &faviconWire{Raster: f.Raster, SVG: f.SVG}
  }
  {{end}}
  ```

  **Las dos ramas.** Olvidar una deja el productor funcionando en la mitad de los
  proyectos y silencioso en la otra, que es el peor resultado posible.

### 2.2 — Sólo el módulo raíz puede declarar icono

Si un módulo que **no** es la raíz declara `Favicon()`, el build **falla**
nombrándolo:

```
sitec: solo el modulo raiz puede declarar Favicon(); lo declara github.com/acme/widget
```

Un icono es la identidad del sitio, no de una librería. Dos librerías con icono
serían dos marcas compitiendo por la misma pestaña, y elegir una en silencio es
la clase de decisión que nadie quiere descubrir en producción.

### 2.3 — Emitir el juego

En `emit_core.go`, cuando el recolector trajo un `Favicon`:

1. `favicon.Derive(favicon.Source{...})` → `[]favicon.File`.
2. Cada archivo se escribe con `am.Write(f.Name, f.Content, f.Mediatype)`, el
   mismo camino que ya usa el binario WASM.
3. Si `Derive` devuelve error —logo no cuadrado, demasiado chico— **el build
   falla con ese error tal cual**. Ya viene redactado para que lo lea una
   persona.

En `emit_html.go`, el `<head>` deja de emitir un enlace fijo y emite **uno por
cada archivo que traiga `Rel`**, en el orden en que vienen:

```html
<link rel="icon" type="image/png" sizes="32x32" href="/icon-32.png">
<link rel="icon" type="image/png" sizes="192x192" href="/icon-192.png">
<link rel="apple-touch-icon" sizes="180x180" href="/apple-touch-icon.png">
<link rel="icon" type="image/svg+xml" href="/favicon.svg">
```

`favicon.ico` se escribe y **no se enlaza**: los navegadores viejos lo piden
solos a la raíz. Los atributos `type` y `sizes` se omiten cuando el archivo
viene con esos campos vacíos — nada de `sizes=""` en la salida.

### 2.4 — Cuando el proyecto no declara icono

Tres ramas, y ninguna escribe un archivo vacío:

| Situación | Qué hace |
|---|---|
| Declara `Favicon()` | §2.3 |
| No declara, pero existe un `favicon.svg` real en la salida | lo deja intacto y emite **sólo** ese enlace — es el camino del demonio de desarrollo, que observa ese archivo en disco (`emit_events.go`) |
| No declara y no hay archivo | **no escribe nada y no emite ningún enlace**, y registra por el log: `sitec: el proyecto no declara Favicon(); las paginas saldran sin icono` |

**No falles el build por un icono ausente.** Es tentador —este repo predica el
fallo ruidoso— y sería desproporcionado: rompería a todos los proyectos del
ecosistema por un archivo decorativo. Lo ruidoso aquí es el aviso, y sobre todo
**dejar de enlazar lo que no existe**: el defecto de hoy no es que falte el
icono, es que la página afirma tenerlo.

### 2.5 — Los consumidores del handler viejo

`faviconSvgHandler` sigue existiendo para el camino de desarrollo, pero deja de
escribirse vacío. Revisa y ajusta sus tres consumidores:

| Archivo | Qué hace hoy | Qué debe hacer |
|---|---|---|
| `emit_route.go:107` | rellena `doc.FaviconURL` con la ruta del handler | usar el primer archivo con `Rel: "icon"`; si no hay ninguno, dejarlo vacío |
| `emit_inspect.go:118` | `GetFaviconURLPath()` | igual que arriba; que no devuelva la ruta de un archivo que no se escribió |
| `emit_events.go` | observa `favicon.svg` en disco | **no lo toques**: es el camino de desarrollo y sigue siendo válido |

## 3. Tests — en `tests/`

`gotest`, nunca `go test`. Ni un `*_test.go` fuera de `tests/`.

| Test | Qué fija |
|---|---|
| `TestFaviconProducerIsRecognized` | El escáner reconoce `Favicon` como productor: un paquete que lo declara aparece con `HasFavicon`. |
| `TestFaviconOnlyRootModule` | Un módulo no raíz que lo declara → error nombrando el módulo. |
| `TestFaviconEmitsFullSet` | Con un logo cuadrado de 256, la salida contiene `icon-32.png`, `icon-192.png`, `apple-touch-icon.png` y `favicon.ico`. |
| `TestFaviconLinksInHead` | El `<head>` lleva un `<link>` por archivo con `Rel`, con sus `type` y `sizes`, y **ninguno** para el `.ico`. |
| `TestFaviconInvalidLogoFailsBuild` | Un logo de 800×600 → el build falla con el mensaje de `favicon.Derive`, sin escribir nada. |
| `TestNoFaviconNoLinkNoFile` | Sin productor y sin archivo: la salida **no** contiene `favicon.svg` y el `<head>` **no** trae ningún `rel="icon"`. |
| `TestExistingFaviconSvgSurvives` | Un `favicon.svg` real en la salida sigue ahí, con su contenido, después del build. Es la regresión que motivó el plan. |

El último es el que hay que escribir primero: hoy falla.

## 4. Documentación

- [`docs/ARCHITECTURE.md`](ARCHITECTURE.md): la lista de productores pasa a
  nueve, con `Favicon()` y su regla de módulo raíz.
- [`README.md`](../README.md): ejemplo de declaración con `go:embed`, y la nota
  de que `sitec` **no sanea** el SVG que reciba: un SVG de un tercero se limpia
  antes con `github.com/tinywasm/svg/sanitize`. El de un proyecto es suyo y es
  de confianza.

Ningún documento debe citar `docs/PLAN.md`: este archivo se borra al publicar.

## 5. Criterios de aceptación

- [ ] `gotest` en verde.
- [ ] `gofmt -l .` vacío.
- [ ] `grep -rn "Favicon" scanner.go select.go extract.go` → los tres reconocen el productor.
- [ ] `grep -c "HasFavicon" extract.go` → **2** como mínimo: las dos ramas de la plantilla.
- [ ] `grep -rn "generateFaviconLink" *.go` → el enlace fijo a `image/svg+xml` ya no existe.
- [ ] Un build de un proyecto sin icono no deja `favicon.svg` en la salida:
      `TestNoFaviconNoLinkNoFile` lo prueba.
- [ ] Los siete tests de §3 existen y pasan.

## 6. Anti-footguns

1. **No derives los tamaños aquí.** Redimensionar y escribir el `.ico` es de
   `github.com/tinywasm/image/favicon`. Este repo decide *cuándo* se llama y
   *cómo* se enlaza.
2. **No sanees SVG aquí.** Un proyecto declara su propio icono, y lo suyo es de
   confianza. El SVG de un desconocido lo limpia
   `github.com/tinywasm/svg/sanitize`, en quien lo recibe.
3. **Las dos ramas de la plantilla de `extract.go`.** Receptor con nombre y
   paquete suelto. Es el error más fácil de cometer aquí.
4. **No conviertas el icono ausente en un error de build.** Ver §2.4.
5. `docs/PLAN.md` (este archivo) no se renombra ni se borra, y su frontmatter
   —`PLAN`, `TAG`, `EXECUTOR`, `STATUS`, `SESSION`, `PR`— **no se edita a mano**.
