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

func TestExtractModule_RootWithSSRInSubpackages(t *testing.T) {
	root := t.TempDir()

	write := func(path, content string) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write("go.mod", `module example.com/app

go 1.24

require (
	github.com/tinywasm/widget v0.1.0
	github.com/tinywasm/js v0.0.4
	github.com/tinywasm/svg v0.1.8
)
`)
	write("main.go", "package main\n\nfunc main() {}\n")

	// Dummy imports to prevent go mod tidy from pruning the dependencies
	write("config/dummy_imports.go", `package config

import (
	_ "github.com/tinywasm/js"
	_ "github.com/tinywasm/svg/sprite"
)
`)

	// El CSS vive en config/, no en la raíz — como en una app real.
	write("config/css.go", `//go:build !wasm

package config

import (
	"github.com/tinywasm/widget"
	"github.com/tinywasm/widget/style"
)

type Config struct{}
func (c *Config) WidgetName() widget.Name { return "config" }
func (c *Config) WidgetKind() widget.Kind { return widget.Region }

func (c *Config) Style() *style.Sheet {
	return style.Of("config").Part("body", style.On(style.Page))
}

func SSR() []widget.Widget {
	return []widget.Widget{&Config{}}
}
`)

	// Y un módulo de dominio aporta su propio CSS, un nivel más abajo.
	write("modules/catalog/css.go", `//go:build !wasm

package catalog

import (
	"github.com/tinywasm/widget"
	"github.com/tinywasm/widget/style"
)

type Catalog struct{}
func (c *Catalog) WidgetName() widget.Name { return "catalog" }
func (c *Catalog) WidgetKind() widget.Kind { return widget.Region }

func (c *Catalog) Style() *style.Sheet {
	return style.Of("catalog").Part("body", style.On(style.Page))
}

func SSR() []widget.Widget {
	return []widget.Widget{&Catalog{}}
}
`)

	// Tidy the module
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\nOutput: %s", err, string(out))
	}

	e := ssr.New(root)
	e.SetLog(t.Log)
	f := modfind.New()
	f.Seed(root, []modfind.Module{{Path: "example.com/app", Dir: root}})
	e.SetFinder(f)

	// Esto es exactamente lo que hace tinywasm/app: extraer desde la RAÍZ.
	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatalf("ExtractModule: %v", err)
	}
	if assets == nil {
		t.Fatal("ExtractModule devolvió nil: la app se queda sin CSS")
	}

	if !strings.Contains(assets.CSS, ".config") {
		t.Errorf("CSS de config/ no extraído.\n  got CSS: %q", assets.CSS)
	}
	if !strings.Contains(assets.CSS, ".catalog") {
		t.Errorf("CSS de modules/catalog/ no extraído.\n  got CSS: %q", assets.CSS)
	}
}
