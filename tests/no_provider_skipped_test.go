//go:build !wasm

package sitec_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tinywasm/modfind"
	"github.com/tinywasm/sitec"
)

func TestExtract_NoProviderSkipped(t *testing.T) {
	root := t.TempDir()

	goMod := `module example.com/app

go 1.25.2
`
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(root, "config"), 0755); err != nil {
		t.Fatal(err)
	}

	// Just a file without any receiver or free function matching SSR features, and doesn't import widget/style
	htmlContent := `//go:build !wasm

package config

// Normal non-SSR function
func Hello() string {
	return "hello"
}
`
	if err := os.WriteFile(filepath.Join(root, "config", "html.go"), []byte(htmlContent), 0644); err != nil {
		t.Fatal(err)
	}

	e := sitec.New(root)
	f := modfind.New()
	f.Seed(root, []modfind.Module{{Path: "example.com/app", Dir: root}})
	e.SetFinder(f)

	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Because config/html.go has no SSR features, it has HasAnyFeature as false.
	// So we should get no assets (or nil assets).
	if assets != nil && (assets.CSS != "" || assets.HTML != "" || assets.RootCSS != "") {
		t.Fatalf("expected nil or empty assets, got: %+v", assets)
	}
}
