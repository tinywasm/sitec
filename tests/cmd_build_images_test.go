//go:build !wasm

package sitec_test

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	imgmin "github.com/tinywasm/image/min"
	"github.com/tinywasm/modfind"
	"github.com/tinywasm/sitec"
)

func TestCmdBuild_ImageProcessing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sitec_img_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create go.mod in tmpDir
	gomodContent := `module example.com/imgtest

go 1.25.2

require (
	github.com/tinywasm/css v0.4.12
	github.com/tinywasm/image v0.0.21
)
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(gomodContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a simple valid PNG image
	imgDir := filepath.Join(tmpDir, "img")
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		t.Fatal(err)
	}

	pngPath := filepath.Join(imgDir, "hero.png")
	m := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for x := 0; x < 10; x++ {
		for y := 0; y < 10; y++ {
			m.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	f, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, m); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	// Create image.go
	imageGoContent := `package myapp

import "github.com/tinywasm/image"

func RenderImages() []image.Asset {
	return []image.Asset{
		{
			Path:     "img/hero.png",
			Variants: image.AllVariants,
			Alt:      "Hero Image",
		},
	}
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "image.go"), []byte(imageGoContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create css.go so sitec extraction succeeds
	cssGoContent := `package myapp

import "github.com/tinywasm/css"

func RenderCSS() *css.Stylesheet {
	return css.NewStylesheet(css.Raw("body{color:red}"))
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "css.go"), []byte(cssGoContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Run go mod tidy in tmpDir so go.sum is populated
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed in tmpDir: %v, output: %s", err, string(out))
	}

	outDir := filepath.Join(tmpDir, "web", "public")

	// Set up Finder seed to avoid running go list in mock module
	fFinder := modfind.New()
	fFinder.Seed(tmpDir, []modfind.Module{
		{Path: "example.com/imgtest", Dir: tmpDir},
	})

	// Set up Extractor and AssetMin mimicking sitec build
	e := sitec.New(tmpDir)
	e.SetFinder(fFinder)
	all, err := e.ExtractAll()
	if err != nil {
		t.Fatalf("ExtractAll failed: %v", err)
	}

	am := sitec.NewAssetMin(&sitec.Config{
		OutputDir: outDir,
		RootDir:   tmpDir,
	})
	am.EnsureOutputDirectoryExists()
	am.SetSSRExtractor(e)
	am.SetFS(sitec.NewOsFS())

	imgHandler := imgmin.New(&imgmin.Config{
		RootDir:   tmpDir,
		OutputDir: filepath.Join(outDir, "img"),
		Quality:   82,
	})
	imgHandler.SetLog(func(msg ...any) { t.Log(msg...) })
	imgHandler.SetFinder(e.Finder())
	am.SetImageProcessor(imgHandler)

	if err := am.RouteExtractedAssets(all); err != nil {
		t.Fatalf("RouteExtractedAssets failed: %v", err)
	}

	if err := imgHandler.LoadImages(); err != nil {
		t.Fatalf("LoadImages failed: %v", err)
	}

	if err := am.FlushToDisk(); err != nil {
		t.Fatalf("FlushToDisk failed: %v", err)
	}

	// Verify optimized image variants exist in outDir/img/
	imgOutDir := filepath.Join(outDir, "img")
	entries, err := os.ReadDir(imgOutDir)
	if err != nil {
		t.Fatalf("failed to read output img dir: %v", err)
	}

	if len(entries) == 0 {
		t.Fatalf("expected processed image files in %s, but directory is empty", imgOutDir)
	}

	// El fixture es un PNG totalmente opaco, así que cada variante sale en
	// .jpg. Este test exigía un .webp, que es exactamente la duplicación que
	// se corrigió: el pipeline emitía las dos codificaciones del mismo origen
	// y la página cargaba una mientras la otra viajaba al CDN sin que nadie la
	// pidiera —y en fotografías la .webp sin pérdida pesaba más que la .jpg.
	var jpg, webp []string
	for _, entry := range entries {
		switch filepath.Ext(entry.Name()) {
		case ".jpg":
			jpg = append(jpg, entry.Name())
		case ".webp":
			webp = append(webp, entry.Name())
		}
	}

	if len(jpg) == 0 {
		t.Errorf("un origen opaco debe emitir variantes .jpg; la salida tiene %v", entries)
	}
	if len(webp) != 0 {
		t.Errorf("un origen opaco no debe emitir además .webp: %v duplican a %v", webp, jpg)
	}
}
