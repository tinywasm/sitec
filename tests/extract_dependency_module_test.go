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

// Una app real toma sus componentes de OTROS módulos (tinywasm/layout, components), y
// el CSS de esos módulos vive en sus subpaquetes (layout/platformd/css.go). ExtractAll
// debe traerlo: si no, la hoja de estilos sale con las variables del proyecto y sin un
// solo estilo de componente — la página se renderiza sin diseño.
func TestExtractAll_DependencyModuleWithSSRInSubpackage(t *testing.T) {
	base := t.TempDir()
	appDir := filepath.Join(base, "app")
	depDir := filepath.Join(base, "layout")

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Módulo de dependencia: su CSS está en un subpaquete, no en la raíz.
	write(filepath.Join(depDir, "go.mod"), `module example.com/layout

go 1.24

require (
	github.com/tinywasm/widget v0.1.0
	github.com/tinywasm/js v0.0.4
	github.com/tinywasm/svg v0.1.8
)
`)
	write(filepath.Join(depDir, "platformd", "dummy_imports.go"), `package platformd

import (
	_ "github.com/tinywasm/js"
	_ "github.com/tinywasm/svg/sprite"
)
`)
	write(filepath.Join(depDir, "platformd", "css.go"), `//go:build !wasm

package platformd

import (
	"github.com/tinywasm/widget"
	"github.com/tinywasm/widget/style"
)

type Platform struct{}
func (p *Platform) WidgetName() widget.Name { return "pd-root" }
func (p *Platform) WidgetKind() widget.Kind { return widget.Region }

func (p *Platform) Style() *style.Sheet {
	return style.Of("pd-root").Part("body", style.On(style.Page))
}

func SSR() []widget.Widget {
	return []widget.Widget{&Platform{}}
}
`)

	// La app lo consume por replace local (como en desarrollo).
	write(filepath.Join(appDir, "go.mod"), `module example.com/app

go 1.24

require (
	example.com/layout v0.0.0
	github.com/tinywasm/widget v0.1.0
	github.com/tinywasm/js v0.0.4
	github.com/tinywasm/svg v0.1.8
)

replace example.com/layout => ../layout
`)
	write(filepath.Join(appDir, "dummy_imports.go"), `package main

import (
	_ "example.com/layout/platformd"
	_ "github.com/tinywasm/js"
	_ "github.com/tinywasm/svg/sprite"
)
`)
	write(filepath.Join(appDir, "main.go"), "package main\n\nfunc main() {}\n")

	// Tidy the dependency module first
	cmdDep := exec.Command("go", "mod", "tidy")
	cmdDep.Dir = depDir
	if out, err := cmdDep.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy in layout failed: %v\nOutput: %s", err, string(out))
	}

	// Tidy the app module
	cmdApp := exec.Command("go", "mod", "tidy")
	cmdApp.Dir = appDir
	if out, err := cmdApp.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy in app failed: %v\nOutput: %s", err, string(out))
	}

	e := ssr.New(appDir)
	e.SetLog(t.Log)
	f := modfind.New()
	f.Seed(appDir, []modfind.Module{
		{Path: "example.com/app", Dir: appDir},
		{Path: "example.com/layout", Dir: depDir},
	})
	e.SetFinder(f)

	all, err := e.ExtractAll()
	if err != nil {
		t.Fatalf("ExtractAll: %v", err)
	}

	var css strings.Builder
	for _, a := range all {
		css.WriteString(a.RootCSS)
		css.WriteString(a.CSS)
	}

	if !strings.Contains(css.String(), "pd-root") {
		t.Errorf("el CSS del módulo de dependencia no se extrajo.\n  got: %q\n"+
			"La app sirve una hoja de estilos sin los estilos de sus componentes.", css.String())
	}
}
