//go:build !wasm

package sitec

import (
	"testing"

	"github.com/tinywasm/js"
)

// White-box (package sitec, repo root) DELIBERADAMENTE — como
// emit_flush_internal_test.go: necesita ver que las rutas relativizadas de
// RouteExtractedAssets NO se resuelven contra una clave absoluta en Read.
//
// BUG capturado: un shell de app WASM (un solo index.html, sin RenderPages)
// sirviéndose DESDE MEMORIA (NewMemFS, diskMirrored=false):
//
//   1. RouteExtractedAssets, al no haber páginas, reescribe
//      mainJsHandler.urlPath / mainStyleCssHandler.urlPath a RELATIVOS
//      ("script.js", "style.css") — correcto para las referencias del HTML.
//   2. El servidor pregunta a Read con la clave ABSOLUTA que llega por HTTP
//      ("/script.js"). El match del bucle allAssets compara
//      a.GetURLPath()=="script.js" con "/script.js" y falla; a.outputPath es
//      "<OutputDir>/script.js" y tampoco coincide con "/script.js".
//   3. processAsset sólo escribe en c.fs `if c.diskMirrored`, así que en
//      modo memoria el fallback c.fs.Read("script.js") tampoco lo encuentra.
//
// Resultado: Read("/script.js") y Read("/style.css") devuelven ok=false. El
// navegador descarga client.wasm y nunca lo instancia (falta el bootstrap);
// #app queda en blanco. index.html se salva porque su urlPath es "/" fijo y
// RouteExtractedAssets no lo toca.
//
// Ningún test previo lo capta: los de flush prueban el camino a disco
// (diskMirrored=true), y los de Read no combinan "sin páginas" +
// "servido desde memoria" + clave absoluta.
func TestCaso1_ReadSirveGlueYCssRelativizadosDesdeMemoria(t *testing.T) {
	am := NewAssetMin(&Config{OutputDir: t.TempDir()})
	am.SetFS(NewMemFS()) // modo memoria: diskMirrored permanece false

	// El demonio registra el bootstrap del WASM así (section-build.go).
	if err := am.UpdateSSRModule("bootstrap", "", []*js.Script{js.PageBootstrap()}, "", nil); err != nil {
		t.Fatalf("UpdateSSRModule(bootstrap): %v", err)
	}

	// Extracción SSR de un shell WASM: un módulo raíz con CSS y SIN páginas.
	if err := am.RouteExtractedAssets([]*Assets{{
		ModuleName: "root",
		RootCSS:    ".fixture{color:red}",
		IsRoot:     true,
	}}); err != nil {
		t.Fatalf("RouteExtractedAssets: %v", err)
	}
	am.RefreshJSAssets()

	// El servidor (sitec/serve) siempre consulta con la clave absoluta que
	// trae la petición HTTP.
	for _, key := range []string{"/script.js", "/style.css"} {
		content, mt, ok := am.Read(key)
		if !ok {
			t.Errorf("BUG C1: Read(%q) ok=false — el shell WASM servido desde memoria no entrega su glue/CSS; el navegador no puede instanciar el .wasm", key)
			continue
		}
		if len(content) == 0 {
			t.Errorf("Read(%q) ok pero content vacío (mt=%q)", key, mt)
		}
	}

	// El bootstrap debe llevar el runtime que instancia el .wasm.
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
