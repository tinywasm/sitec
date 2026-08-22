---
PLAN: "fix: rutas relativas para css/js/favicon/sprite cuando el build no declara paginas"
EXECUTOR: jules
REVIEWER: none
---

> Este plan se despacha con el flujo CodeJob. Ver skill: agents-workflow.

# Plan — `tinywasm/sitec`: rutas absolutas rompen el shell de una app sin páginas

## El bug, confirmado

`sitec` genera el `index.html` de una app (p. ej. `veltylabs/misitio`, el shell
WASM en `web/client.go`) con el CSS y el JS principales enlazados por **ruta
absoluta desde la raíz del dominio**:

```html
<link rel="stylesheet" href="/style.css" type="text/css" />
<script src="/script.js" type="text/javascript"></script>
```

Cuando ese `index.html` no se sirve exactamente desde la raíz del dominio —un
servidor de preview local que lo monta bajo un subpath, por ejemplo— esas rutas
absolutas apuntan al sitio equivocado y el CSS/JS no cargan. Confirmado a mano:
quitar la barra inicial (`href="style.css"`) hace que cargue correctamente.

## La causa

En [`emit_core.go`](../emit_core.go), dentro de `NewAssetMin`:

```go
c.mainStyleCssHandler.urlPath = path.Join("/", ac.AssetsURLPrefix, cssMainFileName)
c.mainJsHandler.urlPath = path.Join("/", ac.AssetsURLPrefix, jsMainFileName)
c.faviconSvgHandler.urlPath = path.Join("/", ac.AssetsURLPrefix, svgFaviconFileName)
c.spriteSvgHandler.urlPath = path.Join("/", ac.AssetsURLPrefix, svgMainFileName)
```

El `path.Join("/", ...)` fuerza una ruta absoluta desde el dominio, sin importar
si `AssetsURLPrefix` está vacío o no.

## Por qué no es tan simple como quitar la barra ahí mismo

Esos mismos cuatro handlers **también** los usa `emitPages` en
[`emit_route.go`](../emit_route.go) para las páginas de un sitio multi-página
(el motor que usa `sitetheme` para publicar el sitio de un cliente):

```go
if doc.CSSURL == "" {
    doc.CSSURL = c.mainStyleCssHandler.GetURLPath()
}
```

Una página ahí puede vivir a cualquier profundidad —
`tests/emit_pages_test.go` ya prueba `Path: "/especialidades/oftalmologia/"`,
que se escribe en `web/public/especialidades/oftalmologia/index.html"`—. Para
esa página, sólo una ruta **absoluta** (`/style.css`) llega al CSS compartido
sin importar la profundidad; una ruta relativa (`style.css`) se resolvería
como `especialidades/oftalmologia/style.css`, que no existe. Ese caso ya
funciona y ya está probado — **no se toca**.

La diferencia real es: **¿este build declara alguna página (`Assets.Pages`) o
es sólo el shell de una app (`RenderHTML`/CSS/JS sin páginas, como
`misitio`)?** Sin páginas, `index.html` es el único documento y siempre vive
en la raíz de salida — la ruta relativa es correcta y además más portátil (no
depende de que el dominio sirva exactamente desde su raíz). Con páginas, se
necesita la ruta absoluta actual.

Esa distinción sólo se conoce en
[`RouteExtractedAssets`](../emit_core.go) (recibe `all []*Assets`, con todas
las páginas de todos los módulos) — no en `NewAssetMin`, que corre antes de
saber si habrá páginas.

## El cambio

En `emit_core.go`, función `RouteExtractedAssets` (la que arranca con
`c.mu.Lock(); defer c.mu.Unlock()`), agrega este bloque **inmediatamente
después** de `defer c.mu.Unlock()` y **antes** del comentario `// 0.
RenderSite():` — sin tocar la numeración de los pasos existentes:

```go
	// Las rutas del CSS/JS/favicon/sprite principales son relativas cuando el
	// build no declara ninguna página: un único index.html en la raíz (el
	// shell de una app WASM, sin RenderPages) puede montarse bajo cualquier
	// prefijo — dominio raíz, subpath de un servidor de preview, etc. — y una
	// ruta relativa lo resuelve sin importar dónde. Absolutas desde "/" sólo
	// son correctas cuando SÍ hay páginas, porque entonces pueden vivir a
	// distinta profundidad (ver TestEmitPages_MultiPageEmission, con una
	// página en "/especialidades/oftalmologia/") y sólo una referencia
	// absoluta desde el dominio las alcanza a todas por igual. Recalculado en
	// cada llamada (no sólo la primera) para que un hot-reload que agrega o
	// quita páginas no deje una ruta obsoleta.
	hasPages := false
	for _, a := range all {
		if a != nil && len(a.Pages) > 0 {
			hasPages = true
			break
		}
	}

	cssFile := filepath.Base(c.mainStyleCssHandler.urlPath)
	jsFile := filepath.Base(c.mainJsHandler.urlPath)
	faviconFile := filepath.Base(c.faviconSvgHandler.urlPath)
	spriteFile := filepath.Base(c.spriteSvgHandler.urlPath)

	if hasPages {
		c.mainStyleCssHandler.urlPath = path.Join("/", c.Config.AssetsURLPrefix, cssFile)
		c.mainJsHandler.urlPath = path.Join("/", c.Config.AssetsURLPrefix, jsFile)
		c.faviconSvgHandler.urlPath = path.Join("/", c.Config.AssetsURLPrefix, faviconFile)
		c.spriteSvgHandler.urlPath = path.Join("/", c.Config.AssetsURLPrefix, spriteFile)
	} else {
		c.mainStyleCssHandler.urlPath = path.Join(c.Config.AssetsURLPrefix, cssFile)
		c.mainJsHandler.urlPath = path.Join(c.Config.AssetsURLPrefix, jsFile)
		c.faviconSvgHandler.urlPath = path.Join(c.Config.AssetsURLPrefix, faviconFile)
		c.spriteSvgHandler.urlPath = path.Join(c.Config.AssetsURLPrefix, spriteFile)
	}

```

`filepath.Base` recupera el nombre de archivo puro sin importar si el estado
actual del campo es absoluto o relativo (así el cálculo es idempotente sin
importar cuántas veces se llame). `path.Join(prefix, file)` con `prefix == ""`
produce `file` sin barra inicial — exactamente `"style.css"`, que es la forma
que ya se comprobó a mano que funciona.

`path` y `filepath` ya están importados en este archivo — no agregues imports.

## Fuera de alcance — no lo toques

- **`emit_route.go`** (`normalizePagePath`, `pageAsset.urlPath`,
  `sitemapAsset.urlPath`): ya hace lo correcto para páginas y sitemap —
  `/sitemap.xml` absoluto es una convención dura de SEO, no un bug.
- **`emit_ssr_register.go`** (`c.standaloneJS[s.Name].urlPath = "/" + s.Name`):
  módulos JS independientes, un problema distinto — no es lo que se reportó.
- **La derivación de favicon multi-archivo** dentro de `RouteExtractedAssets`
  (`favicon.Derive`, `urlKey := path.Join("/", f.Name)`, más abajo en la misma
  función): ruta de código separada, no ligada al bug reportado — `misitio`
  documenta que ese productor todavía no está conectado en ningún proyecto.

## Tests

### 1 — Nuevo: sin páginas, las rutas deben ser relativas

En [`tests/emit_pages_test.go`](../tests/emit_pages_test.go), después de
`TestEmitPages_RenderHTML_Only_Regression` (que ya monta el fixture correcto:
`Assets{ModuleName: ..., HTML: ...}` sin `Pages`):

```go
func TestEmitPages_RenderHTML_Only_RelativeAssetPaths(t *testing.T) {
	ac := &sitec.Config{
		OutputDir: "web/public",
	}
	am := sitec.NewAssetMin(ac)

	htmlAsset := &sitec.Assets{
		ModuleName: "example.com/app",
		HTML:       "<div>App Shell</div>",
	}

	err := am.RouteExtractedAssets([]*sitec.Assets{htmlAsset})
	if err != nil {
		t.Fatalf("unexpected error for RenderHTML-only module: %v", err)
	}

	indexPath := "web/public/index.html"
	indexBytes, _, ok := am.Read(indexPath)
	if !ok {
		t.Fatalf("expected %s to exist", indexPath)
	}
	indexStr := string(indexBytes)

	if !strings.Contains(indexStr, `href="style.css"`) {
		t.Errorf("expected relative stylesheet href when no pages are declared, got: %s", indexStr)
	}
	if !strings.Contains(indexStr, `src="script.js"`) {
		t.Errorf("expected relative script src when no pages are declared, got: %s", indexStr)
	}
	if strings.Contains(indexStr, `href="/style.css"`) || strings.Contains(indexStr, `src="/script.js"`) {
		t.Errorf("asset paths must not be domain-root-absolute when no pages are declared, got: %s", indexStr)
	}
}
```

### 2 — Extiende el test existente: con páginas, las rutas siguen absolutas

En `TestEmitPages_MultiPageEmission` (mismo archivo), justo antes del cierre
de la función (después de la comprobación de `"Oftalmología Content"`), agrega:

```go
	if !strings.Contains(indexStr, `href="/style.css"`) {
		t.Errorf("home page stylesheet must stay domain-root-absolute when pages exist, got: %s", indexStr)
	}
	if !strings.Contains(subStr, `href="/style.css"`) {
		t.Errorf("nested page stylesheet must stay domain-root-absolute when pages exist, got: %s", subStr)
	}
```

Este es el candado que evita que una futura limpieza generalice el cambio de
arriba y rompa el caso multi-página.

## Criterios de aceptación

- [ ] `RouteExtractedAssets` recalcula `urlPath` de los cuatro handlers según
      `hasPages`, exactamente como arriba.
- [ ] `TestEmitPages_RenderHTML_Only_RelativeAssetPaths` pasa: sin páginas,
      `href="style.css"` y `src="script.js"`, sin barra inicial.
- [ ] `TestEmitPages_MultiPageEmission` (extendido) sigue en verde: con
      páginas, `href="/style.css"` absoluto, tanto en la página raíz como en
      la anidada.
- [ ] `go test ./...` en verde, incluida toda la suite existente sin
      modificar — en particular `TestEmitPages_Collision_RenderHTML_and_RenderPages`
      y `TestEmitPages_Collision_DuplicatePages`.
- [ ] `grep -n "path.Join(\"/\", ac.AssetsURLPrefix" emit_core.go` → vacío (la
      asignación de `NewAssetMin` sigue ahí sin cambios, pero ya no es la
      última palabra: `RouteExtractedAssets` la recalcula; si prefieres,
      confirma en su lugar que `RouteExtractedAssets` contiene el bloque
      `hasPages`).

## Fuera de alcance

No se toca `emit_route.go`, `emit_ssr_register.go`, ni la derivación
multi-archivo de favicon — ver "Fuera de alcance" arriba. No se agrega
configuración nueva ni una bandera pública: la decisión es automática, en
función de si el build declara páginas.
