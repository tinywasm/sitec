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

func TestBuild_ConsumerAndInvariants(t *testing.T) {
	t.Run("consumer shaped test", func(t *testing.T) {
		base := t.TempDir()
		appDir := filepath.Join(base, "app")
		depDir := filepath.Join(base, "comp")

		write := func(path, content string) {
			t.Helper()
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
		}

		// Dependency module with CSS and SVG producers
		write(filepath.Join(depDir, "go.mod"), `module example.com/comp

go 1.25.2

require (
	github.com/tinywasm/css v0.4.15
	github.com/tinywasm/svg v0.1.21
)
`)
		write(filepath.Join(depDir, "widget", "css.go"), `//go:build !wasm

package widget

import "github.com/tinywasm/css"

type Widget struct{}

func (w *Widget) RenderCSS() *css.Stylesheet {
	return css.NewStylesheet(
		css.Raw("@layer components { .fixture-widget { display: block; } }"),
	)
}
`)
		write(filepath.Join(depDir, "widget", "svg.go"), `//go:build !wasm

package widget

import (
	"github.com/tinywasm/svg"
	"github.com/tinywasm/svg/sprite"
)

func (w *Widget) IconSvg() *sprite.Sprite {
	return sprite.NewSprite(
		sprite.Define(svg.Icon("fixture-icon"), "0 0 24 24", sprite.Path("M0 0h24v24H0z")),
	)
}
`)

		// App module with RenderPages
		wcwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		repoRoot := filepath.Dir(wcwd)
		write(filepath.Join(appDir, "go.mod"), `module example.com/app

go 1.25.2

require (
	example.com/comp v0.0.0
	github.com/tinywasm/css v0.4.15
	github.com/tinywasm/html v0.0.17
	github.com/tinywasm/sitec v0.0.0
	github.com/tinywasm/svg v0.1.21
)

replace example.com/comp => ../comp

replace github.com/tinywasm/sitec => `+repoRoot+`
`)
		write(filepath.Join(appDir, "app.go"), `package app

import (
	_ "example.com/comp/widget"
	"github.com/tinywasm/html"
)

type PageProducer struct{}

func (p *PageProducer) RenderPages() []html.Page {
	return []html.Page{
		{
			Path: "index.html",
			Doc: html.DocumentOptions{
				Title: "Fixture Site",
			},
			Body: "<div class=\"fixture-widget\"><svg><use href=\"#fixture-icon\"/></svg></div>",
		},
	}
}
`)

		// El raíz declara RenderSite(): el proyecto es un sitio estático con
		// URL pública y un activo de marca que sale verbatim a la salida.
		write(filepath.Join(appDir, "site.go"), `//go:build !wasm

package app

import "github.com/tinywasm/sitec"

func (p *PageProducer) RenderSite() *sitec.Site {
	return &sitec.Site{
		URL:          "https://acme.example",
		StaticAssets: []string{"escudo-marca.svg"},
	}
}
`)
		write(filepath.Join(appDir, "escudo-marca.svg"), `<svg xmlns="http://www.w3.org/2000/svg"><path id="escudo"/></svg>`)

		cmdDep := exec.Command("go", "mod", "tidy")
		cmdDep.Dir = depDir
		if out, err := cmdDep.CombinedOutput(); err != nil {
			t.Fatalf("go mod tidy in depDir failed: %v, output: %s", err, string(out))
		}

		cmdApp := exec.Command("go", "mod", "tidy")
		cmdApp.Dir = appDir
		if out, err := cmdApp.CombinedOutput(); err != nil {
			t.Fatalf("go mod tidy in appDir failed: %v, output: %s", err, string(out))
		}

		site, err := sitec.Build(sitec.BuildConfig{
			RootDir: appDir,
			Mode:    sitec.ModeRelease,
		})
		if err != nil {
			t.Fatalf("sitec.Build failed: %v", err)
		}

		artifacts := site.Artifacts()
		if len(artifacts) == 0 {
			t.Fatalf("expected artifacts, got 0")
		}

		var cssContent string
		var htmlContent string
		var sitemapContent string
		var shieldContent string

		for _, a := range artifacts {
			if strings.HasSuffix(a.Path, "style.css") {
				cssContent = string(a.Content)
			}
			if a.Mediatype == "text/html" {
				htmlContent = string(a.Content)
			}
			if a.Path == "/sitemap.xml" {
				sitemapContent = string(a.Content)
			}
			if a.Path == "/escudo-marca.svg" {
				shieldContent = string(a.Content)
			}
		}

		// Assertions:
		// 1. Emitted CSS contains .fixture-widget
		if !strings.Contains(cssContent, ".fixture-widget") {
			t.Errorf("expected CSS to contain .fixture-widget, got:\n%s", cssContent)
		}

		// 2. Emitted CSS contains @layer
		if !strings.Contains(cssContent, "@layer") {
			t.Errorf("expected CSS to contain @layer, got:\n%s", cssContent)
		}

		// 3. Emitted HTML contains id="fixture-icon"
		if !strings.Contains(htmlContent, `id="fixture-icon"`) {
			t.Errorf("expected HTML to contain id=\"fixture-icon\", got:\n%s", htmlContent)
		}

		// 4. RenderSite() URL emite sitemap.xml
		if !strings.Contains(sitemapContent, "https://acme.example") {
			t.Errorf("expected sitemap.xml to contain the RenderSite() URL, got:\n%s", sitemapContent)
		}

		// 5. El activo estático declarado por RenderSite() sale verbatim
		if shieldContent != `<svg xmlns="http://www.w3.org/2000/svg"><path id="escudo"/></svg>` {
			t.Errorf("expected escudo-marca.svg copied verbatim, got:\n%s", shieldContent)
		}
	})

	t.Run("empty extraction invariant error", func(t *testing.T) {
		dir := t.TempDir()

		gomodContent := `module example.com/empty

go 1.25.2
`
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomodContent), 0644); err != nil {
			t.Fatal(err)
		}

		site, err := sitec.Build(sitec.BuildConfig{
			RootDir: dir,
			Mode:    sitec.ModeRelease,
		})

		if err == nil {
			t.Fatalf("expected error for empty extraction, got nil err")
		}

		if site != nil {
			t.Fatalf("expected nil *Site on error, got %v", site)
		}
	})
}
