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

// TestSiteDeclaracionSeExtrae: RenderSite() declara la URL pública y los
// activos estáticos, y ambos llegan hasta el entregable (sitemap.xml con la
// URL, robots.txt y favicon.ico copiados verbatim).
func TestSiteDeclaracionSeExtrae(t *testing.T) {
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

	// El productor importa sitec: replace a la raíz del repo local, no al
	// módulo publicado (el paquete sitec_test corre con cwd = tests/).
	wcwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(wcwd)
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		t.Fatalf("no repo root above %s: %v", wcwd, err)
	}

	write(filepath.Join(appDir, "go.mod"), `module example.com/app

go 1.25.2

require (
	github.com/tinywasm/css v0.4.15
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
		{
			Path: "index.html",
			Doc:  html.DocumentOptions{Title: "Fixture"},
			Body: "<div class=\"fixture-widget\">hola</div>",
		},
	}
}

func (p *PageProducer) RenderSite() *sitec.Site {
	return &sitec.Site{
		URL:          "https://acme.example",
		StaticAssets: []string{"robots.txt", "favicon.ico"},
	}
}
`)
	write(filepath.Join(appDir, "robots.txt"), "User-agent: *\nDisallow: /admin\n")
	write(filepath.Join(appDir, "favicon.ico"), "fake-ico-bytes")

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = appDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v, output: %s", err, string(out))
	}

	out, err := sitec.Build(sitec.BuildConfig{
		RootDir: appDir,
		Mode:    sitec.ModeRelease,
	})
	if err != nil {
		t.Fatalf("sitec.Build failed: %v", err)
	}

	found := map[string]bool{}
	var sitemap, robots string
	for _, a := range out.Artifacts() {
		found[a.Path] = true
		if a.Path == "/sitemap.xml" {
			sitemap = string(a.Content)
		}
		if a.Path == "/robots.txt" {
			robots = string(a.Content)
		}
	}

	if !found["/robots.txt"] {
		t.Errorf("expected /robots.txt artifact, got none")
	}
	if !strings.Contains(robots, "Disallow: /admin") {
		t.Errorf("robots.txt debe copiarse verbatim, got %q", robots)
	}
	if !found["/favicon.ico"] {
		t.Errorf("expected /favicon.ico artifact, got none")
	}
	if sitemap == "" {
		t.Fatalf("expected /sitemap.xml artifact when RenderSite() declares URL")
	}
	if !strings.Contains(sitemap, "https://acme.example") {
		t.Errorf("sitemap debe usar la URL declarada por RenderSite(), got:\n%s", sitemap)
	}
}
