package ssr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tinywasm/ssr"
)

func TestExtractAll_Empty(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\ngo 1.24\n"), 0644)
	e := ssr.New(root)
	e.SetListModulesFn(func(string) ([]string, error) { return []string{"example.com/demo"}, nil })
	all, err := e.ExtractAll()
	if err != nil {
		t.Fatal(err)
	}
	_ = all
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
