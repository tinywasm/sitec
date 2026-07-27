//go:build !wasm

package ssr_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinywasm/ssr"
)

func TestExtract_WithRealFinderAndExternalDependencies(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns go run and uses real network; skipped with -short")
	}

	root := t.TempDir()

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("go.mod", `module example.com/realfindtest

go 1.24

require (
	github.com/tinywasm/widget v0.1.0
)
`)
	write("main.go", "package main\n\nfunc main() {}\n")

	// We have a widget that actually implements style.Styler and exports SSR()
	write("components/css.go", `//go:build !wasm

package components

import (
	"github.com/tinywasm/widget"
	"github.com/tinywasm/widget/style"
)

type Button struct{}
func (b *Button) WidgetName() widget.Name { return "btn" }
func (b *Button) WidgetKind() widget.Kind { return widget.Region }

func (b *Button) Style() *style.Sheet {
	return style.Of("btn").Part("label", style.On(style.Page))
}

func SSR() []widget.Widget {
	return []widget.Widget{&Button{}}
}
`)

	// Tidy the temp module (using standard go mod tidy which will resolve github.com/tinywasm/widget)
	// This also adds required transitive dependencies like github.com/tinywasm/css!
	// This represents a real dependency graph containing tinywasm/css, tinywasm/js, etc.
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\nOutput: %s", err, string(out))
	}

	// Create extractor with a REAL (unseeded) finder!
	// This uses the real modfind discovery, mimicking production behavior.
	e := ssr.New(root)
	e.SetLog(t.Log)

	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatalf("ExtractModule failed on real finder: %v", err)
	}
	if assets == nil {
		t.Fatal("expected non-nil assets")
	}

	if !strings.Contains(assets.CSS, "btn") {
		t.Fatalf("CSS was not extracted: %s", assets.CSS)
	}
}
