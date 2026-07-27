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

func TestExtract_StyleExtraction(t *testing.T) {
	root := t.TempDir()

	goMod := `module example.com/app

go 1.25.2

require (
	github.com/tinywasm/widget v0.1.0
)
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
	"github.com/tinywasm/widget"
	"github.com/tinywasm/widget/style"
)

type MyWidget struct{}

func (m *MyWidget) WidgetName() widget.Name { return widget.Name("my-widget") }
func (m *MyWidget) WidgetKind() widget.Kind { return widget.Region }
func (m *MyWidget) Style() *style.Sheet {
	return style.Of(m.WidgetName()).Root(style.Pad(style.Space0))
}
`
	if err := os.WriteFile(filepath.Join(root, "config", "css.go"), []byte(cssContent), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v, output: %s", err, string(out))
	}

	e := ssr.New(root)
	f := modfind.New()
	f.Seed(root, []modfind.Module{{Path: "example.com/app", Dir: root}})
	e.SetFinder(f)

	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatalf("extraction failed: %v", err)
	}
	if assets == nil {
		t.Fatal("expected non-nil assets")
	}
	if !strings.Contains(assets.CSS, "my-widget") {
		t.Fatalf("expected CSS from widget's Style(), got: %q", assets.CSS)
	}
}
