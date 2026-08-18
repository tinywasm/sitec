//go:build !wasm

package sitec_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinywasm/router/mock"
	"github.com/tinywasm/sitec"
)

func TestRegisterRoutes(t *testing.T) {
	t.Run("registers asset routes", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		am := sitec.NewAssetMin(setup.ac)

		// Add some content to trigger cache generation
		if err := am.NewFileEvent("test.js", ".js", setup.createTempFile("test.js", "var a=1;"), "create"); err != nil {
			t.Fatalf("Error processing JS creation: %v", err)
		}
		if err := am.NewFileEvent("test.css", ".css", setup.createTempFile("test.css", "body{}"), "create"); err != nil {
			t.Fatalf("Error processing CSS creation: %v", err)
		}

		r := newTestRouter(am)

		// Invoke GET /style.css and GET /script.js to verify actual bytes served
		ctxCSS := &mock.Context{InPath: "/style.css", InMethod: "GET"}
		r.Invoke("GET", "/style.css", ctxCSS)
		if !strings.Contains(string(ctxCSS.ResponseBody()), "body{}") {
			t.Errorf("expected body{} in CSS response, got %q", string(ctxCSS.ResponseBody()))
		}

		ctxJS := &mock.Context{InPath: "/script.js", InMethod: "GET"}
		r.Invoke("GET", "/script.js", ctxJS)
		if !strings.Contains(string(ctxJS.ResponseBody()), "a=1") {
			t.Errorf("expected a=1 in JS response, got %q", string(ctxJS.ResponseBody()))
		}
	})

	t.Run("registers assets with prefix", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		setup.ac.AssetsURLPrefix = "/static/"
		am := sitec.NewAssetMin(setup.ac)

		if err := am.NewFileEvent("test.js", ".js", setup.createTempFile("test.js", "var b=2;"), "create"); err != nil {
			t.Fatalf("Error processing JS creation: %v", err)
		}

		r := newTestRouter(am)

		ctxJS := &mock.Context{InPath: "/static/script.js", InMethod: "GET"}
		r.Invoke("GET", "/static/script.js", ctxJS)
		if !strings.Contains(string(ctxJS.ResponseBody()), "b=2") {
			t.Errorf("expected b=2 at /static/script.js, got %q", string(ctxJS.ResponseBody()))
		}
	})

	t.Run("sprite is NOT exposed as route", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		am := sitec.NewAssetMin(setup.ac)

		r := newTestRouter(am)

		ctxSprite := &mock.Context{InPath: "/icons.svg", InMethod: "GET"}
		r.Invoke("GET", "/icons.svg", ctxSprite)
		if string(ctxSprite.ResponseBody()) != "404 no encontrado" {
			t.Errorf("expected 404 no encontrado for sprite route, got %q", string(ctxSprite.ResponseBody()))
		}
	})
}

func TestSirveElEstadoActualNoElDelRegistro(t *testing.T) {
	setup := newTestSetup(t)
	defer setup.cleanup()

	am := sitec.NewAssetMin(setup.ac)

	f1 := setup.createTempFile("test1.css", ".a{color:red}")
	if err := am.NewFileEvent("test1.css", ".css", f1, "create"); err != nil {
		t.Fatalf("NewFileEvent test1.css: %v", err)
	}

	r := newTestRouter(am)

	f2 := setup.createTempFile("test2.css", ".b{color:blue}")
	if err := am.NewFileEvent("test2.css", ".css", f2, "create"); err != nil {
		t.Fatalf("NewFileEvent test2.css: %v", err)
	}

	if err := am.Write("especialidades/index.html", []byte("<h1>Especialidades</h1>"), "text/html"); err != nil {
		t.Fatalf("am.Write especialidades/index.html: %v", err)
	}

	ctxCSS := &mock.Context{InPath: "/style.css", InMethod: "GET"}
	r.Invoke("GET", "/style.css", ctxCSS)
	if !strings.Contains(string(ctxCSS.ResponseBody()), ".b{color:blue}") {
		t.Errorf("expected updated CSS content, got %q", string(ctxCSS.ResponseBody()))
	}

	ctxPage := &mock.Context{InPath: "/especialidades/", InMethod: "GET"}
	r.Invoke("GET", "/especialidades/", ctxPage)
	if string(ctxPage.ResponseBody()) != "<h1>Especialidades</h1>" {
		t.Errorf("expected HTML content, got %q", string(ctxPage.ResponseBody()))
	}
}

func TestUnArchivoAusenteEs404NoLaPortada(t *testing.T) {
	setup := newTestSetup(t)
	defer setup.cleanup()

	am := sitec.NewAssetMin(setup.ac)

	r := newTestRouter(am)

	ctx := &mock.Context{InPath: "/img/no-existe.svg", InMethod: "GET"}
	r.Invoke("GET", "/img/no-existe.svg", ctx)

	if string(ctx.ResponseBody()) != "404 no encontrado" {
		t.Errorf("expected '404 no encontrado', got %q", string(ctx.ResponseBody()))
	}
}

func TestElSpriteNoSeExponeComoRecurso(t *testing.T) {
	setup := newTestSetup(t)
	defer setup.cleanup()

	am := sitec.NewAssetMin(setup.ac)

	r := newTestRouter(am)

	ctx := &mock.Context{InPath: "/icons.svg", InMethod: "GET"}
	r.Invoke("GET", "/icons.svg", ctx)

	if string(ctx.ResponseBody()) != "404 no encontrado" {
		t.Errorf("expected '404 no encontrado', got %q", string(ctx.ResponseBody()))
	}
}

func TestAssetMinWriteServiblePorHTTP(t *testing.T) {
	setup := newTestSetup(t)
	defer setup.cleanup()

	am := sitec.NewAssetMin(setup.ac)

	if err := am.Write("client.wasm", []byte("wasm-bytes"), "application/wasm"); err != nil {
		t.Fatalf("am.Write client.wasm failed: %v", err)
	}

	r := newTestRouter(am)

	ctx := &mock.Context{InPath: "/client.wasm", InMethod: "GET"}
	r.Invoke("GET", "/client.wasm", ctx)

	if string(ctx.ResponseBody()) != "wasm-bytes" {
		t.Errorf("expected 'wasm-bytes', got %q", string(ctx.ResponseBody()))
	}
	if ctx.GetHeader("Content-Type") != "application/wasm" {
		t.Errorf("expected Content-Type 'application/wasm', got %q", ctx.GetHeader("Content-Type"))
	}
}

func TestSpriteInjectedInHTML(t *testing.T) {
	setup := newTestSetup(t)
	defer setup.cleanup()

	am := sitec.NewAssetMin(setup.ac)

	// Inject an icon
	err := am.InjectSpriteIcon("test-icon", "<path d='M0 0h1'/>", "0 0 16 16")
	if err != nil {
		t.Fatalf("InjectSpriteIcon failed: %v", err)
	}

	r := newTestRouter(am)

	// Invoke the index handler with mock context
	ctx := &mock.Context{
		InPath:   "/",
		InMethod: "GET",
	}
	r.Invoke("GET", "/", ctx)

	htmlContent := string(ctx.ResponseBody())
	if !strings.Contains(htmlContent, "test-icon") {
		t.Error("index.html should contain injected icon ID")
	}
	if !strings.Contains(htmlContent, "<svg") {
		t.Error("index.html should contain <svg> tag for sprite")
	}
}

func TestWorks(t *testing.T) {
	t.Run("default does not write to disk", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		am := sitec.NewAssetMin(setup.ac)

		err := am.NewFileEvent("test.css", ".css", setup.createTempFile("test.css", "body{color:red}"), "create")
		if err != nil {
			t.Fatalf("Error processing CSS creation: %v", err)
		}

		// Check file does NOT exist
		_, err = os.Stat(filepath.Join(setup.outputDir, "style.css"))
		if !os.IsNotExist(err) {
			t.Errorf("File style.css should NOT exist when FlushToDisk has not been called")
		}
	})

	t.Run("FlushToDisk enables disk mirroring", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		am := sitec.NewAssetMin(setup.ac)
		if err := am.FlushToDisk(); err != nil {
			t.Fatalf("FlushToDisk: %v", err)
		}

		err := am.NewFileEvent("test.css", ".css", setup.createTempFile("test.css", "body{color:red}"), "create")
		if err != nil {
			t.Fatalf("Error processing CSS creation: %v", err)
		}

		// Check file EXISTS
		content, err := os.ReadFile(filepath.Join(setup.outputDir, "style.css"))
		if err != nil {
			t.Fatalf("Failed to read output CSS: %v", err)
		}
		if string(content) != "body{color:red}" {
			t.Errorf("File style.css: expected body{color:red}, got %q", string(content))
		}
	})

	t.Run("HTML link and script tags respect URL prefix", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		setup.ac.AssetsURLPrefix = "/assets"
		am := sitec.NewAssetMin(setup.ac)

		r := newTestRouter(am)

		// Invoke the index handler
		ctx := &mock.Context{
			InPath:   "/",
			InMethod: "GET",
		}
		r.Invoke("GET", "/", ctx)

		htmlContent := string(ctx.ResponseBody())
		if !strings.Contains(htmlContent, `href="/assets/style.css"`) {
			t.Errorf("HTML should contain href=\"/assets/style.css\"")
		}
		if !strings.Contains(htmlContent, `src="/assets/script.js"`) {
			t.Errorf("HTML should contain src=\"/assets/script.js\"")
		}
	})

	t.Run("InjectBodyContent is served in index.html", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		am := sitec.NewAssetMin(setup.ac)

		am.InjectHTML("<div id='custom'>Injected</div>")

		r := newTestRouter(am)

		// Invoke the index handler
		ctx := &mock.Context{
			InPath:   "/",
			InMethod: "GET",
		}
		r.Invoke("GET", "/", ctx)

		htmlContent := string(ctx.ResponseBody())
		if !strings.Contains(htmlContent, "<div id='custom'>Injected</div>") {
			t.Error("index.html should contain injected content")
		}
	})
}
