//go:build !wasm

package sitec_test

import (
	"os"
	"path/filepath"
	"testing"

	imgmin "github.com/tinywasm/image/min"
	"github.com/tinywasm/router/mock"
	"github.com/tinywasm/sitec"
)

type stubImageProcessor struct {
	artifacts []imgmin.Artifact
}

func (s *stubImageProcessor) UnobservedFiles() []string {
	return nil
}

func (s *stubImageProcessor) Artifacts() []imgmin.Artifact {
	return s.artifacts
}

func TestPublishImagesHaceServibleUnaImagen(t *testing.T) {
	setup := newTestSetup(t)
	defer setup.cleanup()

	am := sitec.NewAssetMin(setup.ac)
	am.SetFS(sitec.NewMemFS())

	ip := &stubImageProcessor{
		artifacts: []imgmin.Artifact{
			{
				Path:      "img/foto.jpg",
				Mediatype: "image/jpeg",
				Content:   []byte("fake-jpeg-bytes"),
			},
		},
	}
	am.SetImageProcessor(ip)

	if err := am.PublishImages(); err != nil {
		t.Fatalf("PublishImages failed: %v", err)
	}

	content, mediatype, ok := am.Read("img/foto.jpg")
	if !ok {
		t.Fatalf("am.Read failed for img/foto.jpg")
	}
	if string(content) != "fake-jpeg-bytes" {
		t.Errorf("expected 'fake-jpeg-bytes', got %q", string(content))
	}
	if mediatype != "image/jpeg" {
		t.Errorf("expected 'image/jpeg', got %q", mediatype)
	}

	r := newTestRouter(am)
	ctx := &mock.Context{
		InPath:   "/img/foto.jpg",
		InMethod: "GET",
	}
	r.Invoke("GET", "/img/foto.jpg", ctx)

	if string(ctx.ResponseBody()) != "fake-jpeg-bytes" {
		t.Errorf("expected HTTP response body 'fake-jpeg-bytes', got %q", string(ctx.ResponseBody()))
	}
	if ctx.GetHeader("Content-Type") != "image/jpeg" {
		t.Errorf("expected Content-Type 'image/jpeg', got %q", ctx.GetHeader("Content-Type"))
	}
}

// TestPublishImagesEscribeBajoOutputDir prueba con el FS por defecto (osFS,
// el que usa el demonio en producción, no NewMemFS()): antes de este test,
// PublishImages escribía en fs.Write(a.Path, ...) sin anclar a OutputDir, así
// que osFS —que trata rutas relativas como relativas al cwd del proceso—
// dejaba el archivo en <cwd>/img/foto.jpg en vez de <OutputDir>/img/foto.jpg.
// Con el demonio corriendo desde la raíz del proyecto del usuario, eso creaba
// un directorio de salida fantasma ahí mismo.
func TestPublishImagesEscribeBajoOutputDir(t *testing.T) {
	fakeCwd := t.TempDir()
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(fakeCwd); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalCwd)

	outputDir := t.TempDir()
	ac := &sitec.Config{OutputDir: outputDir}
	am := sitec.NewAssetMin(ac)

	ip := &stubImageProcessor{
		artifacts: []imgmin.Artifact{
			{
				Path:      "img/foto.jpg",
				Mediatype: "image/jpeg",
				Content:   []byte("fake-jpeg-bytes"),
			},
		},
	}
	am.SetImageProcessor(ip)

	if err := am.PublishImages(); err != nil {
		t.Fatalf("PublishImages failed: %v", err)
	}

	wantPath := filepath.Join(outputDir, "img", "foto.jpg")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("se esperaba la imagen en %s (bajo OutputDir): %v", wantPath, err)
	}

	strayPath := filepath.Join(fakeCwd, "img", "foto.jpg")
	if _, err := os.Stat(strayPath); err == nil {
		t.Errorf("PublishImages escribió fuera de OutputDir, en el cwd del proceso: %s", strayPath)
	}
}
