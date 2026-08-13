package ssr_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinywasm/modfind"
	"github.com/tinywasm/ssr"
)

func TestExtractAll_Empty(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\ngo 1.24\n"), 0644)
	e := ssr.New(root)
	f := modfind.New()
	f.Seed(root, []modfind.Module{{Path: "example.com/demo", Dir: root}})
	e.SetFinder(f)
	_, err := e.ExtractAll()
	if err == nil {
		t.Fatal("expected error on empty result set")
	}
	if !strings.Contains(err.Error(), "no assets extracted from any module") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtractModule_NoSSRFiles(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\ngo 1.24\n"), 0644)
	e := ssr.New(root)
	a, err := e.ExtractModule(root)
	if err != nil {
		t.Fatal(err)
	}
	if a != nil {
		t.Error("expected nil for module with no SSR files")
	}
}
