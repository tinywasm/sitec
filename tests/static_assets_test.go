//go:build !wasm

package sitec_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinywasm/sitec"
)

// TestStaticAssetsUnionSinDuplicados: BuildConfig.StaticAssets se une a los de
// RenderSite() en un solo recorrido — una ruta declarada en ambas fuentes sale
// una sola vez, no dos copias del mismo artefacto.
func TestStaticAssetsUnionSinDuplicados(t *testing.T) {
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
		{Path: "index.html", Doc: html.DocumentOptions{Title: "Fixture"}},
	}
}

func (p *PageProducer) RenderSite() *sitec.Site {
	return &sitec.Site{
		StaticAssets: []string{"robots.txt"},
	}
}
`)
	write(filepath.Join(appDir, "robots.txt"), "from-render-site\n")
	write(filepath.Join(appDir, "nota.txt"), "from-build-config\n")

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = appDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v, output: %s", err, string(out))
	}

	out, err := sitec.Build(sitec.BuildConfig{
		RootDir:      appDir,
		Mode:         sitec.ModeRelease,
		StaticAssets: []string{"robots.txt", "nota.txt"},
	})
	if err != nil {
		t.Fatalf("sitec.Build failed: %v", err)
	}

	count := 0
	nota := false
	for _, a := range out.Artifacts() {
		switch a.Path {
		case "/robots.txt":
			count++
			if string(a.Content) != "from-render-site\n" {
				t.Errorf("/robots.txt debe salir una sola vez; got %d, contenido %q", count, a.Content)
			}
		case "/nota.txt":
			nota = true
		}
	}
	if count != 1 {
		t.Errorf("robots.txt declarado en ambas fuentes debe emitirse una sola vez, got %d", count)
	}
	if !nota {
		t.Errorf("expected /nota.txt artifact (BuildConfig.StaticAssets), got none")
	}
}

// TestStaticAssetsAusenteEsError: un activo estático declarado (por RenderSite
// o BuildConfig) y ausente del disco es error de build — un logo que falta se
// descubre en compilación, no en producción.
func TestStaticAssetsAusenteEsError(t *testing.T) {
	appDir := t.TempDir()

	wcwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(wcwd)

	gomod := `module example.com/app

go 1.25.2

require (
	github.com/tinywasm/html v0.0.17
	github.com/tinywasm/sitec v0.0.0
)

replace github.com/tinywasm/sitec => ` + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}
	app := `package app

import (
	"github.com/tinywasm/html"
	"github.com/tinywasm/sitec"
)

type PageProducer struct{}

func (p *PageProducer) RenderPages() []html.Page {
	return []html.Page{
		{Path: "index.html", Doc: html.DocumentOptions{Title: "Fixture"}},
	}
}

func (p *PageProducer) RenderSite() *sitec.Site {
	return &sitec.Site{
		StaticAssets: []string{"logo-que-no-existe.svg"},
	}
}
`
	if err := os.WriteFile(filepath.Join(appDir, "app.go"), []byte(app), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = appDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v, output: %s", err, string(out))
	}

	_, err = sitec.Build(sitec.BuildConfig{
		RootDir: appDir,
		Mode:    sitec.ModeRelease,
	})
	if err == nil {
		t.Fatal("un activo estático declarado y ausente debe fallar el build, got nil")
	}
	if !strings.Contains(err.Error(), "logo-que-no-existe.svg") {
		t.Errorf("el error debe nombrar el activo ausente, got: %v", err)
	}
}
