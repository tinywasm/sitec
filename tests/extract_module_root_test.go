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

// TestExtractModule_RootWithSSRInSubpackages reproduce un bug real: una app cuyo
// css.go/svg.go viven en SUBPAQUETES (config/, modules/x/) y no en la raíz del
// módulo se quedaba SIN CSS y SIN iconos — `web/public/style.css` se escribía con 0
// bytes y la página salía sin estilos.
//
// La detección de features leía los ssrSourceFiles SOLO en la raíz del módulo
// (`os.ReadFile(filepath.Join(m.dir, f))`), así que para un módulo cuyo css.go está
// en config/ no encontraba nada: HasRoot=false, y el main.go generado nunca llamaba
// a RootCSS().
//
// El test hermano (TestExtractModule_Subpackage) pasa el directorio DEL SUBPAQUETE y
// pasaba en verde, tapando el agujero: nadie probaba lo que hace `tinywasm/app`, que
// llama a ReloadSSRModule con la RAÍZ del proyecto.
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

	write("go.mod", "module example.com/app\n\ngo 1.24\n")
	write("main.go", "package main\n\nfunc main() {}\n")

	// El CSS vive en config/, no en la raíz — como en una app real.
	write("config/css.go", `//go:build !wasm

package config

type stylesheet string

func (s stylesheet) String() string { return string(s) }

func RootCSS() stylesheet { return stylesheet(":root{--brand:red}") }
`)

	// Y un módulo de dominio aporta su propio CSS, un nivel más abajo.
	write("modules/catalog/css.go", `//go:build !wasm

package catalog

type stylesheet string

func (s stylesheet) String() string { return string(s) }

type Catalog struct{}

func (c *Catalog) RenderCSS() stylesheet { return stylesheet(".catalog{display:grid}") }
`)

	e := sitec.New(root)
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

	if !strings.Contains(assets.RootCSS, "--brand:red") {
		t.Errorf("RootCSS de config/ no extraído.\n  got RootCSS: %q\n"+
			"La hoja de estilos se escribe vacía y la página se renderiza sin estilos.", assets.RootCSS)
	}
	if !strings.Contains(assets.CSS, ".catalog{display:grid}") {
		t.Errorf("CSS de modules/catalog/ no extraído.\n  got CSS: %q", assets.CSS)
	}
}
