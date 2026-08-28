---
PLAN: "fix: AssetMin.Read resuelve rutas relativizadas contra la clave absoluta de la petición (shell WASM servido desde memoria)"
EXECUTOR: jules
REVIEWER: none
---

> Este plan se despacha con el flujo CodeJob. Ver skill: `agents-workflow`.
> No ejecutes `gopush` ni `codejob`.

# PLAN — `AssetMin.Read`: rutas relativas vs. clave absoluta

## Prerrequisito

```bash
go install github.com/tinywasm/devflow/cmd/gotest@latest
```

Usá `gotest` (no `go test`).

## El bug, observado

Un **shell de app WASM** (un solo `index.html`, sin `RenderPages`) servido
**desde memoria** por el demonio de `tinywasm/app` (proyecto sin
`web/server.go` → `SetFS(NewMemFS())`, `diskMirrored == false`):

```
GET /            → 200
GET /client.wasm → 200
GET /script.js   → 404   ← el bootstrap que instancia el .wasm
GET /style.css   → 404
```

Sin `/script.js` el navegador descarga `client.wasm` y **nunca lo instancia**;
`#app` queda en blanco. `index.html` se salva porque su `urlPath` es `"/"`
fijo.

## Causa raíz (dos factores que se combinan)

1. **`RouteExtractedAssets` relativiza** (`emit_core.go`, rama `else` de
   `hasPages`): cuando el build no declara páginas, reescribe
   `mainStyleCssHandler.urlPath` / `mainJsHandler.urlPath` /
   `faviconSvgHandler.urlPath` / `spriteSvgHandler.urlPath` a **relativos**
   (`"script.js"`, `"style.css"`, sin barra inicial). Es correcto y
   deliberado: las referencias del HTML de un shell montable bajo cualquier
   prefijo se resuelven relativas.

2. **`AssetMin.Read` compara mal** (`emit_core.go`, bucle `for _, a := range
   c.allAssets`): la petición HTTP siempre llega **absoluta**
   (`p == "/script.js"`, `urlKey == "/script.js"`). El match hace
   `a.GetURLPath() == urlKey` → `"script.js" == "/script.js"` → falso;
   `aURL == cleanURL` → falso; `a.outputPath == p` →
   `"<OutputDir>/script.js" == "/script.js"` → falso. Ningún caso especial
   cubre css/js/favicon/sprite. → cae al fallback `c.fs.Read` → el `MemFS`
   está vacío (`processAsset` sólo escribe en `c.fs` `if c.diskMirrored`, y en
   modo memoria es `false`) → **404**.

`index.html` no se ve afectado: `indexHtmlHandler.urlPath` se fija a `"/"` en
`NewAssetMin` y `RouteExtractedAssets` no lo toca.

## Por qué ningún test lo captaba

- `emit_flush_internal_test.go` / `tests/emit_flush_to_disk_test.go`: prueban
  el camino **a disco** (`FlushToDisk`, `diskMirrored == true`).
- Los tests de `Read`: ninguno combina las tres condiciones a la vez —
  **sin páginas** (⇒ `RouteExtractedAssets` relativiza) + **`NewMemFS`**
  (⇒ sin fallback a disco) + **clave absoluta** (⇒ el mismatch aflora).
- En `tinywasm/app`, `tests/memory_serve_test.go` sólo pide `/img/*`, nunca
  `/script.js` ni `/style.css`.

## Etapa 1 — El test que falla capturando el bug (YA ESCRITO, verificá)

El archivo **`caso1_serve_internal_test.go`** (raíz del repo, `package sitec`
white-box, `//go:build !wasm` — misma excepción deliberada a la convención
`tests/` que `emit_flush_internal_test.go`) ya existe con este contenido.
**Verificá que existe tal cual y que FALLA contra el código actual**
(`gotest -run TestCaso1_ReadSirveGlueYCssRelativizadosDesdeMemoria`):

```go
//go:build !wasm

package sitec

import (
	"testing"

	"github.com/tinywasm/js"
)

func TestCaso1_ReadSirveGlueYCssRelativizadosDesdeMemoria(t *testing.T) {
	am := NewAssetMin(&Config{OutputDir: t.TempDir()})
	am.SetFS(NewMemFS()) // modo memoria: diskMirrored permanece false

	if err := am.UpdateSSRModule("bootstrap", "", []*js.Script{js.PageBootstrap()}, "", nil); err != nil {
		t.Fatalf("UpdateSSRModule(bootstrap): %v", err)
	}
	if err := am.RouteExtractedAssets([]*Assets{{
		ModuleName: "root",
		RootCSS:    ".fixture{color:red}",
		IsRoot:     true,
	}}); err != nil {
		t.Fatalf("RouteExtractedAssets: %v", err)
	}
	am.RefreshJSAssets()

	for _, key := range []string{"/script.js", "/style.css"} {
		content, mt, ok := am.Read(key)
		if !ok {
			t.Errorf("BUG C1: Read(%q) ok=false — el shell WASM servido desde memoria no entrega su glue/CSS", key)
			continue
		}
		if len(content) == 0 {
			t.Errorf("Read(%q) ok pero content vacío (mt=%q)", key, mt)
		}
	}
	if content, _, ok := am.Read("/script.js"); ok {
		s := string(content)
		if !containsAny(s, "WebAssembly", "instantiate", "wasm_exec") {
			t.Errorf("/script.js no contiene el bootstrap del WASM:\n%.300s", s)
		}
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}
```

Si el archivo no está o no falla, **PARÁ y REPORTÁ** — el resto del plan
asume que este test es el criterio.

## Etapa 2 — El fix en `AssetMin.Read`

En `emit_core.go`, función `func (c *AssetMin) Read(p string) ([]byte, string, bool)`,
dentro del bucle `for _, a := range c.allAssets`. Justo después de calcular
`aURL` (el bloque que hace `aURL := a.GetURLPath()` + el `TrimRight` de la
barra final), agregá la normalización de barra inicial y sumala al `if` del
match:

```go
		aURL := a.GetURLPath()
		if strings.HasSuffix(aURL, "/") && aURL != "/" {
			aURL = strings.TrimRight(aURL, "/")
		}
		// RouteExtractedAssets guarda las rutas de css/js/favicon/sprite
		// RELATIVAS ("script.js") cuando el build no declara páginas —
		// correcto para las referencias del HTML de un shell WASM montable
		// bajo cualquier prefijo. La petición HTTP siempre llega absoluta
		// ("/script.js"), así que se comparan sin la barra inicial.
		aURLAbs := aURL
		if !strings.HasPrefix(aURLAbs, "/") {
			aURLAbs = "/" + aURLAbs
		}

		if a.GetURLPath() == urlKey || aURL == cleanURL || aURLAbs == cleanURL || a.outputPath == p ||
			(a.GetURLPath() == "/" && (urlKey == "/index.html" || (outDir != "" && p == filepath.Join(outDir, "index.html")))) ||
			(strings.HasSuffix(urlKey, "/") && urlKey != "/" && (a.GetURLPath() == urlKey+"index.html" || (outDir != "" && a.outputPath == filepath.Join(outDir, strings.TrimPrefix(urlKey, "/")+"index.html")))) {
```

El único cambio funcional es el nuevo `|| aURLAbs == cleanURL` en la condición
(más las 4 líneas que calculan `aURLAbs`). No toques el resto del match ni el
fallback `c.fs.Read` de más abajo.

**Verificado localmente:** con este cambio, el test de la Etapa 1 pasa y
`gotest ./...` queda en verde (35 s, `race ✅ tests ✅`).

## Etapa 3 — Defensa en profundidad en `processAsset` (opcional pero recomendado)

En `emit_events.go`, `func (c *AssetMin) processAsset(fh *asset) error`:

```go
func (c *AssetMin) processAsset(fh *asset) error {
	if err := fh.RegenerateCache(c.activeMinifier()); err != nil {
		return err
	}
	if c.diskMirrored {
		return c.fs.Write(fh.outputPath, fh.GetCachedMinified(), fh.mediatype)
	}
	return nil
}
```

`c.diskMirrored` sólo debe gobernar si `c.fs` es un **espejo de disco real**
(`OsFS`), no si se escribe. En modo memoria `c.fs` es un `MemFS` y escribir en
él **no toca el proyecto** — es exactamente como ya funcionan las imágenes
(`directArtifacts`). Cambiá la guarda para escribir siempre que haya FS:

```go
	if c.fs != nil {
		return c.fs.Write(fh.outputPath, fh.GetCachedMinified(), fh.mediatype)
	}
	return nil
```

**Anti-footgun:** si esto rompe algún test de flush (los que cuentan
escrituras a disco o afirman "en caso 1 no se escribe ni un byte" — esos
tests son de `tinywasm/app`, no de este repo, pero verificá los de
`tests/emit_flush_to_disk_test.go`), **revertí SÓLO la Etapa 3** y quedate con
la Etapa 2, que ya arregla el bug por sí sola. La Etapa 2 es la corrección
mínima suficiente; la 3 es robustez.

## Etapa 4 — Documentación

- `docs/ASSETMIN_HTTP_HANDLERS.md` y/o `docs/ASSETMIN_API.md`: si describen
  cómo `Read`/el handler HTTP resuelven una ruta, agregá una frase: las
  rutas de `script.js`/`style.css`/`icons.svg`/`favicon.svg` se guardan
  **relativas** cuando el build no declara páginas (shell WASM), y `Read` las
  resuelve contra la clave absoluta de la petición sin la barra inicial.
- **No** cites `docs/PLAN.md` desde ningún documento permanente.

## Restricciones del repo

- `tinywasm/sitec` compila con Go estándar (no es un binario WASM del Worker);
  `strings`/`path`/`filepath` están permitidos aquí — ya se usan en `Read`.
- Idioma: identificadores y mensajes de error en inglés; comentarios de prosa
  y docs en español (los comentarios de este plan ya están así).
- **El árbol tiene cambios sin commitear ajenos a este bug** (`build.go`,
  `emit_flush.go`, `reach.go`, `select.go`, `extract.go`, `pipeline.go`,
  `tests/emit_flush_to_disk_test.go`). Este plan **sólo** toca
  `emit_core.go` (Etapa 2), opcionalmente `emit_events.go` (Etapa 3),
  `caso1_serve_internal_test.go` (ya existe) y `docs/` (Etapa 4). No toques
  los demás archivos.

## Criterios de aceptación

- [ ] `caso1_serve_internal_test.go` existe y **pasa** tras la Etapa 2.
- [ ] `gotest ./...` en verde (con `-race`, `-vet`).
- [ ] `grep -n "aURLAbs" emit_core.go` → aparece sólo dentro de `Read`.
- [ ] El único cambio de comportamiento es que `Read("/script.js")` /
      `Read("/style.css")` / `Read("/icons.svg")` / `Read("/favicon.svg")`
      devuelven `ok=true` para un `AssetMin` con `NewMemFS()` y assets
      relativizados (sin páginas). Ninguna ruta que ya resolvía cambia.
- [ ] Ningún archivo de la lista "ajenos a este bug" aparece en el diff.

## Seguimiento en `tinywasm/app` (fuera de este repo, no lo hagas acá)

`tinywasm/app` tiene `tests/caso1_trio_servido_test.go` (end-to-end: arranca
el demonio en caso 1 y pide `/`, `/script.js`, `/style.css`, `/client.wasm`).
Falla hoy por este mismo bug. Cuando esta versión de `sitec` publique,
`tinywasm/app` bumpea la dependencia y ese test pasa — es un plan aparte en
ese repo.
