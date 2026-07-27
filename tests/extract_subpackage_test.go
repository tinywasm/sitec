//go:build !wasm

package ssr_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinywasm/modfind"
	"github.com/tinywasm/ssr"
)

func TestExtractModule_Subpackage(t *testing.T) {
	parentDir := t.TempDir()

	parentGomod := `module example.com/parent

go 1.24

require (
	github.com/tinywasm/widget v0.1.0
	github.com/tinywasm/js v0.0.4
	github.com/tinywasm/svg v0.1.8
)
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

	// Dummy imports to prevent go mod tidy from pruning the dependencies
	if err := os.WriteFile(filepath.Join(subDir, "dummy_imports.go"), []byte(`package sub

import (
	_ "github.com/tinywasm/js"
	_ "github.com/tinywasm/svg/sprite"
)
`), 0644); err != nil {
		t.Fatal(err)
	}

	subSSR := `//go:build !wasm

package sub

import (
	"github.com/tinywasm/widget"
	"github.com/tinywasm/widget/style"
)

type Sub struct{}

func (s *Sub) WidgetName() widget.Name { return "sub" }
func (s *Sub) WidgetKind() widget.Kind { return widget.Region }

func (s *Sub) Style() *style.Sheet {
	return style.Of("sub").Part("body", style.On(style.Page))
}

func SSR() []widget.Widget {
	return []widget.Widget{&Sub{}}
}
`
	if err := os.WriteFile(filepath.Join(subDir, "css.go"), []byte(subSSR), 0644); err != nil {
		t.Fatalf("write sub/css.go: %v", err)
	}

	// Tidy the parent module
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = parentDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\nOutput: %s", err, string(out))
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

	if !strings.Contains(assets.CSS, ".sub") {
		t.Fatalf("subpackage CSS not extracted\n  got:  %q\n  module: %q",
			assets.CSS, assets.ModuleName)
	}

	const wantModuleName = "example.com/parent"
	if assets.ModuleName != wantModuleName {
		t.Fatalf("expected ModuleName to be resolved to owner module\n  want: %q\n  got:  %q",
			wantModuleName, assets.ModuleName)
	}
}
