//go:build !wasm

package sitec_test

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinywasm/sitec"
)

// TestReleaseIncluyeImagen: un proyecto que declara RenderImages() y
// RenderSite() entrega sus imágenes en el conjunto de artefactos del release
// (Output.Artifacts), sin que nada tenga que volcarlas por separado.
//
// Regresión del defecto §7: PublishImages solo alimentaba el FS del demonio
// —el release no llamaba LoadImages→PublishImages, así que una imagen
// "publicada" jamás aparecía en el entregable.
func TestReleaseIncluyeImagen(t *testing.T) {
	appDir := t.TempDir()

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	wcwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(wcwd)

	write(filepath.Join(appDir, "go.mod"), `module example.com/app

go 1.25.2

require (
	github.com/tinywasm/html v0.0.17
	github.com/tinywasm/image v0.0.21
	github.com/tinywasm/sitec v0.0.0
)

replace github.com/tinywasm/sitec => `+repoRoot+`
`)
	write(filepath.Join(appDir, "app.go"), `package app

import (
	"github.com/tinywasm/html"
	"github.com/tinywasm/sitec"
)

type PageProducer struct{}

func (p *PageProducer) RenderPages() []html.Page {
	return []html.Page{
		{Path: "index.html", Doc: html.DocumentOptions{Title: "Fixture"}, Body: "<img src=\"/img/hero.jpg\" alt=\"\">"},
	}
}

func (p *PageProducer) RenderSite() *sitec.Site {
	return &sitec.Site{
		URL: "https://acme.example",
	}
}
`)
	// El pipeline de imágenes de sitec descubrve los activos en image.go
	// (convención del ecosistema): RenderImages en otro archivo no se detecta.
	write(filepath.Join(appDir, "image.go"), `package app

import "github.com/tinywasm/image"

func RenderImages() []image.Asset {
	return []image.Asset{
		{
			Path:     "img/hero.png",
			Variants: image.AllVariants,
			Alt:      "Hero",
		},
	}
}
`)

	imgDir := filepath.Join(appDir, "img")
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		t.Fatal(err)
	}
	m := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for x := 0; x < 10; x++ {
		for y := 0; y < 10; y++ {
			m.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	f, err := os.Create(filepath.Join(imgDir, "hero.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, m); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = appDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v, output: %s", err, string(out))
	}

	out, err := sitec.Build(sitec.BuildConfig{
		RootDir: appDir,
		Mode:    sitec.ModeRelease,
		Log:     func(msgs ...any) { t.Log(msgs...) },
		// Absoluto para que el caché de conversión quede en el TempDir del
		// test, nunca relativo al cwd del proceso (invariante §6: un test no
		// deja archivos en el repo).
		OutputDir: filepath.Join(appDir, "web", "public"),
	})
	if err != nil {
		t.Fatalf("sitec.Build failed: %v", err)
	}

	var imageArtifact bool
	for _, a := range out.Artifacts() {
		if strings.HasPrefix(a.Path, "/img/") && a.Mediatype == "image/jpeg" && len(a.Content) > 0 {
			imageArtifact = true
		}
	}
	if !imageArtifact {
		t.Fatalf("el release debe incluir la imagen procesada como artefacto /img/*.jpg")
	}
}
