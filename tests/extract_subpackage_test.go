//go:build !wasm

package ssr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tinywasm/modfind"
	"github.com/tinywasm/ssr"
)

func TestExtractModule_Subpackage(t *testing.T) {
	parentDir := t.TempDir()

	parentGomod := `module example.com/parent

go 1.24
`
	if err := os.WriteFile(filepath.Join(parentDir, "go.mod"), []byte(parentGomod), 0644); err != nil {
		t.Fatalf("write parent go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parentDir, "parent.go"), []byte("package parent\n"), 0644); err != nil {
		t.Fatalf("write parent.go: %v", err)
	}

	subDir := filepath.Join(parentDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	subSSR := `//go:build !wasm

package sub

type stylesheet string

func (s stylesheet) String() string { return string(s) }

type Sub struct{}

func (s *Sub) RenderCSS() stylesheet { return stylesheet(".sub{color:red}") }
`
	if err := os.WriteFile(filepath.Join(subDir, "css.go"), []byte(subSSR), 0644); err != nil {
		t.Fatalf("write sub/css.go: %v", err)
	}

	e := ssr.New(parentDir)
	e.SetLog(t.Log)
	// Mock list modules to include the parent module only, simulating go list -m
	f := modfind.New()
	f.Seed(parentDir, []modfind.Module{{Path: "example.com/parent", Dir: parentDir}})
	e.SetFinder(f)

	assets, err := e.ExtractModule(subDir)
	if err != nil {
		t.Fatalf("ExtractModule returned error: %v", err)
	}
	if assets == nil {
		t.Fatal("ExtractModule returned nil assets")
	}

	const want = ".sub{color:red}"
	if assets.CSS != want {
		t.Fatalf("subpackage CSS not extracted\n  want: %q\n  got:  %q\n  module: %q",
			want, assets.CSS, assets.ModuleName)
	}
}
