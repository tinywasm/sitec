//go:build !wasm

package sitec_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinywasm/modfind"
	"github.com/tinywasm/sitec"
)

func TestExtractAll_RenderPages_EndToEnd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sitec_extract_pages_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create go.mod
	gomodContent := `module example.com/clinic

go 1.25.2

require (
	github.com/tinywasm/html v0.0.17
)
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(gomodContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create pages.go declaring RenderPages() on a struct receiver
	pagesGoContent := `package clinic

import "github.com/tinywasm/html"

type SpecialtyPages struct{}

func (s *SpecialtyPages) RenderPages() []html.Page {
	return []html.Page{
		{
			Path: "/",
			Doc: html.DocumentOptions{
				Title:       "Clínica Chillán — Inicio",
				Description: "Servicios médicos integrales en Chillán",
			},
			Body: "<h1>Bienvenido a Clínica Chillán</h1>",
		},
		{
			Path: "/especialidades/oftalmologia/",
			Doc: html.DocumentOptions{
				Title:       "Oftalmología — Clínica Chillán",
				Description: "Consulta y diagnóstico oftalmológico en Chillán",
			},
			Body: "<h1>Especialidad de Oftalmología</h1>",
		},
	}
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "pages.go"), []byte(pagesGoContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Run go mod tidy in tmpDir so go.sum is populated
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed in tmpDir: %v, output: %s", err, string(out))
	}

	// Set up seed finder so modfind succeeds in tmpDir
	finder := modfind.New()
	finder.Seed(tmpDir, []modfind.Module{
		{Path: "example.com/clinic", Dir: tmpDir},
	})

	e := sitec.New(tmpDir)
	e.SetFinder(finder)

	all, err := e.ExtractAll()
	if err != nil {
		t.Fatalf("ExtractAll failed for RenderPages module: %v", err)
	}

	if len(all) == 0 {
		t.Fatalf("expected extracted assets, got empty")
	}

	// Verify that Pages were extracted into Assets
	var rootAsset *sitec.Assets
	for _, a := range all {
		if a.ModuleName == "example.com/clinic" {
			rootAsset = a
			break
		}
	}

	if rootAsset == nil {
		t.Fatalf("expected asset for example.com/clinic")
	}

	if len(rootAsset.Pages) != 2 {
		t.Fatalf("expected 2 extracted pages in Assets.Pages, got %d", len(rootAsset.Pages))
	}

	p1 := rootAsset.Pages[0]
	if p1.Path != "/" || p1.Doc.Title != "Clínica Chillán — Inicio" {
		t.Errorf("unexpected first page: %+v", p1)
	}

	p2 := rootAsset.Pages[1]
	if p2.Path != "/especialidades/oftalmologia/" || p2.Doc.Title != "Oftalmología — Clínica Chillán" {
		t.Errorf("unexpected second page: %+v", p2)
	}

	// Now route extracted assets with AssetMin and verify emitted HTML
	outDir := filepath.Join(tmpDir, "web", "public")
	ac := &sitec.Config{
		OutputDir: outDir,
		SiteURL:   "https://clinicachillan.cl",
	}
	am := sitec.NewAssetMin(ac)

	if err := am.RouteExtractedAssets(all); err != nil {
		t.Fatalf("RouteExtractedAssets failed: %v", err)
	}

	// Verify index.html
	indexPath := filepath.Join(outDir, "index.html")
	indexBytes, _, ok := am.Read(indexPath)
	if !ok {
		t.Fatalf("expected %s to exist in allAssets", indexPath)
	}
	indexStr := string(indexBytes)
	if !strings.Contains(indexStr, "Clínica Chillán — Inicio") {
		t.Errorf("index.html missing title, got: %s", indexStr)
	}

	// Verify especialidades/oftalmologia/index.html
	subPath := filepath.Join(outDir, "especialidades/oftalmologia/index.html")
	subBytes, _, ok := am.Read(subPath)
	if !ok {
		t.Fatalf("expected %s to exist in allAssets", subPath)
	}
	subStr := string(subBytes)
	if !strings.Contains(subStr, "Oftalmología — Clínica Chillán") {
		t.Errorf("subpage index.html missing title, got: %s", subStr)
	}

	// Verify sitemap.xml
	sitemapPath := filepath.Join(outDir, "sitemap.xml")
	sitemapBytes, _, ok := am.Read(sitemapPath)
	if !ok {
		t.Fatalf("expected %s to exist", sitemapPath)
	}
	sitemapStr := string(sitemapBytes)
	if !strings.Contains(sitemapStr, "<loc>https://clinicachillan.cl/</loc>") ||
		!strings.Contains(sitemapStr, "<loc>https://clinicachillan.cl/especialidades/oftalmologia/</loc>") {
		t.Errorf("sitemap missing expected URLs, got: %s", sitemapStr)
	}
}
