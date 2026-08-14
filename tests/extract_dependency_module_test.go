//go:build !wasm

package sitec_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinywasm/modfind"
	"github.com/tinywasm/sitec"
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
	write(filepath.Join(depDir, "go.mod"), "module example.com/layout\n\ngo 1.24\n")
	write(filepath.Join(depDir, "platformd", "css.go"), `//go:build !wasm

package platformd

type stylesheet string

func (s stylesheet) String() string { return string(s) }

type Platform struct{}

func (p *Platform) RenderCSS() stylesheet { return stylesheet(".pd-root{display:grid}") }
`)

	// La app lo consume por replace local (como en desarrollo).
	write(filepath.Join(appDir, "go.mod"), `module example.com/app

go 1.24

require example.com/layout v0.0.0

replace example.com/layout => ../layout
`)
	write(filepath.Join(appDir, "main.go"), "package main\n\nimport _ \"example.com/layout/platformd\"\n\nfunc main() {}\n")

	e := sitec.New(appDir)
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

	if !strings.Contains(css.String(), ".pd-root{display:grid}") {
		t.Errorf("el CSS del módulo de dependencia no se extrajo.\n  got: %q\n"+
			"La app sirve una hoja de estilos sin los estilos de sus componentes.", css.String())
	}
}
