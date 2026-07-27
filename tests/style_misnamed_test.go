//go:build !wasm

package ssr_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinywasm/modfind"
	"github.com/tinywasm/ssr"
)

func TestExtract_StyleMisnamed(t *testing.T) {
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

	cssContent := `//go:build !wasm

package config

import (
	"github.com/tinywasm/widget/style"
)

type MyWidget struct{}

// Misnamed Style() as GenerateCSS()
func (m *MyWidget) GenerateCSS() *style.Sheet {
	return nil
}
`
	if err := os.WriteFile(filepath.Join(root, "config", "css.go"), []byte(cssContent), 0644); err != nil {
		t.Fatal(err)
	}

	e := ssr.New(root)
	f := modfind.New()
	f.Seed(root, []modfind.Module{{Path: "example.com/app", Dir: root}})
	e.SetFinder(f)

	_, err := e.ExtractModule(root)
	if err == nil {
		t.Fatal("expected an error due to misnamed Style() method")
	}

	expectedStr := "imports github.com/tinywasm/widget/style but declares no Style() method"
	if !strings.Contains(err.Error(), expectedStr) {
		t.Fatalf("expected error message to contain: %q, but got: %v", expectedStr, err)
	}
}
