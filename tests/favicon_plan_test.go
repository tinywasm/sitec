//go:build !wasm

package sitec_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinywasm/html"
	"github.com/tinywasm/sitec"
)

func pngBytesHelper(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 0x09, G: 0x6b, B: 0xf0, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func writeTempAppWithFaviconHelper(t *testing.T, appDir string, raster []byte, svg []byte) {
	t.Helper()
	wcwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(wcwd)
	write := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(appDir, "go.mod"), `module example.com/app

go 1.25.2

require (
	github.com/tinywasm/html v0.0.17
	github.com/tinywasm/image v0.1.0
	github.com/tinywasm/sitec v0.0.0
)

replace github.com/tinywasm/sitec => `+repoRoot+`
`)
	hasSVG := len(svg) > 0
	svgEmbed := ""
	svgField := ""
	if hasSVG {
		write(filepath.Join(appDir, "logo.svg"), string(svg))
		svgEmbed = "//go:embed logo.svg\nvar logoSVG []byte\n"
		svgField = "SVG: logoSVG,"
	}
	rasterFile := filepath.Join(appDir, "logo.png")
	if err := os.WriteFile(rasterFile, raster, 0644); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(appDir, "brand.go"), `package app

import (
	_ "embed"
	"github.com/tinywasm/css"
	"github.com/tinywasm/image/favicon"
)

//go:embed logo.png
var logo []byte
`+svgEmbed+`
type Brand struct{}

func (b *Brand) Favicon() favicon.Source {
	return favicon.Source{Raster: logo, `+svgField+`}
}

func (b *Brand) RenderCSS() *css.Stylesheet { return css.NewStylesheet() }
`)
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = appDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v %s", err, string(out))
	}
}

func TestFaviconProducerIsRecognized(t *testing.T) {
	appDir := t.TempDir()
	raster := pngBytesHelper(t, 256, 256)
	writeTempAppWithFaviconHelper(t, appDir, raster, nil)
	out, err := sitec.Build(sitec.BuildConfig{RootDir: appDir, Mode: sitec.ModeRelease})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	found := false
	for _, a := range out.Artifacts() {
		if a.Path == "/icon-32.png" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected /icon-32.png artifact after Favicon producer, got none; artifacts: %v", out.Artifacts())
	}
}

func TestFaviconOnlyRootModule(t *testing.T) {
	am := sitec.NewAssetMin(&sitec.Config{RootDir: "/tmp/fake-root"})
	am.SetLog(func(...any) {})
	raster := pngBytesHelper(t, 256, 256)
	err := am.RouteExtractedAssets([]*sitec.Assets{
		{ModuleName: "example.com/app", IsRoot: true},
		{ModuleName: "github.com/acme/widget", IsRoot: false, Favicon: &sitec.FaviconWire{Raster: raster}},
	})
	if err == nil {
		t.Fatalf("expected error when non-root declares Favicon(), got nil")
	}
	if !strings.Contains(err.Error(), "github.com/acme/widget") {
		t.Errorf("expected error to name github.com/acme/widget, got: %v", err)
	}
	if !strings.Contains(err.Error(), "only the root module") {
		t.Errorf("expected msgFaviconNonRoot, got: %v", err)
	}
}

func TestFaviconOnlyRootModuleViaBuild(t *testing.T) {
	wcwd, _ := os.Getwd()
	repoRoot := filepath.Dir(wcwd)
	appDir := t.TempDir()
	libDir := filepath.Join(appDir, "libwidget")
	write := func(p, c string) {
		os.MkdirAll(filepath.Dir(p), 0755)
		os.WriteFile(p, []byte(c), 0644)
	}
	write(filepath.Join(appDir, "go.mod"), `module example.com/app

go 1.25.2

require (
	github.com/tinywasm/html v0.0.17
	github.com/tinywasm/image v0.1.0
	github.com/tinywasm/sitec v0.0.0
	example.com/widget v0.0.0
)

replace github.com/tinywasm/sitec => `+repoRoot+`
replace example.com/widget => ./libwidget
`)
	raster := pngBytesHelper(t, 256, 256)
	os.WriteFile(filepath.Join(appDir, "logo.png"), raster, 0644)
	write(filepath.Join(appDir, "app.go"), `package app

import (
	_ "embed"
	"github.com/tinywasm/css"
	"github.com/tinywasm/image/favicon"
	_ "example.com/widget"
)

//go:embed logo.png
var logo []byte

type Brand struct{}
func (b *Brand) Favicon() favicon.Source { return favicon.Source{Raster: logo} }
func (b *Brand) RenderCSS() *css.Stylesheet { return css.NewStylesheet() }
`)
	write(filepath.Join(libDir, "go.mod"), `module example.com/widget

go 1.25.2

require (
	github.com/tinywasm/css v0.4.15
	github.com/tinywasm/image v0.1.0
)
`)
	write(filepath.Join(libDir, "widget.go"), `package widget

import (
	_ "embed"
	"github.com/tinywasm/css"
	"github.com/tinywasm/image/favicon"
)

//go:embed logo.png
var logo []byte

type Widget struct{}
func (w *Widget) Favicon() favicon.Source { return favicon.Source{Raster: logo} }
func (w *Widget) RenderCSS() *css.Stylesheet { return css.NewStylesheet() }
`)
	os.WriteFile(filepath.Join(libDir, "logo.png"), raster, 0644)
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = appDir
	cmd.CombinedOutput()
	cmd2 := exec.Command("go", "mod", "tidy")
	cmd2.Dir = libDir
	cmd2.CombinedOutput()
	_, err := sitec.Build(sitec.BuildConfig{RootDir: appDir, Mode: sitec.ModeRelease})
	if err == nil {
		t.Fatalf("expected error when non-root declares Favicon(), got nil")
	}
	if !strings.Contains(err.Error(), "only the root module") {
		t.Errorf("expected error naming module and msgFaviconNonRoot, got: %v", err)
	}
	if !strings.Contains(err.Error(), "example.com/widget") && !strings.Contains(err.Error(), "widget") {
		t.Errorf("expected error to name widget module, got: %v", err)
	}
}

func TestFaviconEmitsFullSet(t *testing.T) {
	appDir := t.TempDir()
	raster := pngBytesHelper(t, 256, 256)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle cx="50" cy="50" r="40"/></svg>`)
	writeTempAppWithFaviconHelper(t, appDir, raster, svg)
	out, err := sitec.Build(sitec.BuildConfig{RootDir: appDir, Mode: sitec.ModeRelease})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := []string{"/icon-32.png", "/icon-192.png", "/apple-touch-icon.png", "/favicon.ico", "/favicon.svg"}
	found := map[string]bool{}
	for _, a := range out.Artifacts() {
		found[a.Path] = true
	}
	for _, w := range want {
		if !found[w] {
			t.Errorf("expected %s in artifacts, got %v", w, found)
		}
	}
}

func TestFaviconLinksInHead(t *testing.T) {
	appDir := t.TempDir()
	raster := pngBytesHelper(t, 256, 256)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle cx="50" cy="50" r="40"/></svg>`)
	writeTempAppWithFaviconHelper(t, appDir, raster, svg)
	out, err := sitec.Build(sitec.BuildConfig{RootDir: appDir, Mode: sitec.ModeRelease})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var htmlContent string
	for _, a := range out.Artifacts() {
		if a.Path == "/" {
			htmlContent = string(a.Content)
		}
	}
	if htmlContent == "" {
		t.Fatalf("no / artifact")
	}
	if !strings.Contains(htmlContent, `rel="icon"`) {
		t.Errorf("head missing icon links: %s", htmlContent)
	}
	if !strings.Contains(htmlContent, `/icon-32.png`) {
		t.Errorf("missing icon-32 link: %s", htmlContent)
	}
	if !strings.Contains(htmlContent, `/icon-192.png`) {
		t.Errorf("missing icon-192 link: %s", htmlContent)
	}
	if !strings.Contains(htmlContent, `apple-touch-icon`) || !strings.Contains(htmlContent, `/apple-touch-icon.png`) {
		t.Errorf("missing apple-touch-icon link: %s", htmlContent)
	}
	if !strings.Contains(htmlContent, `/favicon.svg`) {
		t.Errorf("missing favicon.svg link: %s", htmlContent)
	}
	if strings.Contains(htmlContent, `favicon.ico`) {
		t.Errorf("head should not link favicon.ico, got: %s", htmlContent)
	}
	if !strings.Contains(htmlContent, `type="image/png"`) {
		t.Errorf("expected type image/png for png icons: %s", htmlContent)
	}
	if !strings.Contains(htmlContent, `sizes="32x32"`) {
		t.Errorf("expected sizes 32x32: %s", htmlContent)
	}
}

func TestFaviconInvalidLogoFailsBuild(t *testing.T) {
	appDir := t.TempDir()
	raster := pngBytesHelper(t, 800, 600)
	writeTempAppWithFaviconHelper(t, appDir, raster, nil)
	_, err := sitec.Build(sitec.BuildConfig{RootDir: appDir, Mode: sitec.ModeRelease})
	if err == nil {
		t.Fatalf("expected error for 800x600 logo, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cuadrado") && !strings.Contains(msg, "256") {
		t.Errorf("expected favicon.Derive error message, got: %v", msg)
	}
}

func TestNoFaviconNoLinkNoFile(t *testing.T) {
	appDir := t.TempDir()
	wcwd, _ := os.Getwd()
	repoRoot := filepath.Dir(wcwd)
	write := func(p, c string) {
		os.MkdirAll(filepath.Dir(p), 0755)
		os.WriteFile(p, []byte(c), 0644)
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
	"github.com/tinywasm/css"
	"github.com/tinywasm/html"
)

type P struct{}
func (p *P) RenderPages() []html.Page { return []html.Page{{Path: "/", Doc: html.DocumentOptions{Title: "hi"}, Body: "hi"}} }
func (p *P) RenderCSS() *css.Stylesheet { return css.NewStylesheet() }
`)
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = appDir
	cmd.CombinedOutput()
	out, err := sitec.Build(sitec.BuildConfig{RootDir: appDir, Mode: sitec.ModeRelease})
	if err != nil {
		t.Fatalf("Build without favicon should not fail, got: %v", err)
	}
	for _, a := range out.Artifacts() {
		if a.Path == "/favicon.svg" {
			t.Errorf("should not emit favicon.svg when no Favicon() and no existing file, got artifact %v", a.Path)
		}
		if a.Path == "/icon-32.png" {
			t.Errorf("should not emit icon-32 when no favicon")
		}
	}
	var htmlContent string
	for _, a := range out.Artifacts() {
		if a.Path == "/" {
			htmlContent = string(a.Content)
		}
	}
	if strings.Contains(htmlContent, `rel="icon"`) || strings.Contains(htmlContent, `rel='icon'`) || strings.Contains(htmlContent, `apple-touch-icon`) {
		t.Errorf("head should contain no icon links when no favicon, got: %s", htmlContent)
	}
	_ = html.Page{}
}

func TestExistingFaviconSvgSurvives(t *testing.T) {
	appDir := t.TempDir()
	wcwd, _ := os.Getwd()
	repoRoot := filepath.Dir(wcwd)
	write := func(p, c string) {
		os.MkdirAll(filepath.Dir(p), 0755)
		os.WriteFile(p, []byte(c), 0644)
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
	"github.com/tinywasm/css"
	"github.com/tinywasm/html"
)

type P struct{}
func (p *P) RenderPages() []html.Page { return []html.Page{{Path: "/", Doc: html.DocumentOptions{Title: "hi"}, Body: "hi"}} }
func (p *P) RenderCSS() *css.Stylesheet { return css.NewStylesheet() }
`)
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = appDir
	cmd.CombinedOutput()
	faviconPath := filepath.Join(appDir, "web", "public", "favicon.svg")
	os.MkdirAll(filepath.Dir(faviconPath), 0755)
	original := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle cx="50" cy="50" r="40" fill="red"/></svg>`
	os.WriteFile(faviconPath, []byte(original), 0644)
	out, err := sitec.Build(sitec.BuildConfig{RootDir: appDir, Mode: sitec.ModeRelease, OutputDir: "web/public"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	b, err := os.ReadFile(faviconPath)
	if err != nil {
		t.Fatalf("favicon.svg should survive on disk: %v", err)
	}
	if string(b) != original {
		t.Errorf("favicon.svg content changed, expected %q got %q", original, string(b))
	}
	var htmlContent string
	for _, a := range out.Artifacts() {
		if a.Path == "/" {
			htmlContent = string(a.Content)
		}
	}
	if !strings.Contains(htmlContent, `/favicon.svg`) {
		t.Errorf("head should link existing favicon.svg, got: %s", htmlContent)
	}
	count := strings.Count(htmlContent, `rel="icon"`) + strings.Count(htmlContent, `rel='icon'`) + strings.Count(htmlContent, `rel=\"icon\"`)
	if count != 1 {
		t.Errorf("expected 1 icon link for existing svg, got %d: %s", count, htmlContent)
	}
}
