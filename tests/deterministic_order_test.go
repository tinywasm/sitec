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

// writeFixtureApp creates a realistic consumer app layout:
//
//	root/
//	  go.mod            (module example.com/app)
//	  config/css.go     (config widget style)
//	  modules/alpha/css.go
//	  modules/beta/css.go
//	  modules/zeta/css.go
//
// Each package declares a distinct CSS marker so the merged output order
// can be asserted byte-for-byte.
func writeFixtureApp(t *testing.T) string {
	t.Helper()
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

	write("go.mod", `module example.com/app

go 1.24

require (
	github.com/tinywasm/widget v0.1.0
	github.com/tinywasm/js v0.0.4
	github.com/tinywasm/svg v0.1.8
)
`)

	// Dummy imports to prevent go mod tidy from pruning the dependencies
	write("config/dummy_imports.go", `package config

import (
	_ "github.com/tinywasm/js"
	_ "github.com/tinywasm/svg/sprite"
)
`)

	write("config/css.go", `//go:build !wasm

package config

import (
	"github.com/tinywasm/widget"
	"github.com/tinywasm/widget/style"
)

type Theme struct{}
func (t *Theme) WidgetName() widget.Name { return "config" }
func (t *Theme) WidgetKind() widget.Kind { return widget.Region }

func (t *Theme) Style() *style.Sheet {
	return style.Of("config").Part("body", style.On(style.Page))
}

func SSR() []widget.Widget {
	return []widget.Widget{&Theme{}}
}
`)

	componentCSS := func(pkg, name string) string {
		return `//go:build !wasm

package ` + pkg + `

import (
	"github.com/tinywasm/widget"
	"github.com/tinywasm/widget/style"
)

type Component struct{}
func (c *Component) WidgetName() widget.Name { return "` + name + `" }
func (c *Component) WidgetKind() widget.Kind { return widget.Region }

func (c *Component) Style() *style.Sheet {
	return style.Of("` + name + `").Part("body", style.On(style.Page))
}

func SSR() []widget.Widget {
	return []widget.Widget{&Component{}}
}
`
	}

	write("modules/alpha/css.go", componentCSS("alpha", "alpha"))
	write("modules/beta/css.go", componentCSS("beta", "beta"))
	write("modules/zeta/css.go", componentCSS("zeta", "zeta"))

	// Run go mod tidy in the temporary directory to resolve dependencies and generate go.sum
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\nOutput: %s", err, string(out))
	}

	return root
}

func newSeededExtractor(root string) *ssr.Extractor {
	e := ssr.New(root)
	f := modfind.New()
	f.Seed(root, []modfind.Module{{Path: "example.com/app", Dir: root, IsMain: true}})
	e.SetFinder(f)
	return e
}

// TestExtract_DeterministicAcrossRuns verifies the hypothesis that a Go map
// with random iteration order inside tinywasm/ssr shuffles the extracted CSS
// between process runs. Each iteration uses a FRESH Extractor (empty cache),
// which forces a full generate+`go run` extraction cycle — the same thing
// that happens every time the consumer application restarts.
//
// If this test passes repeatedly, ssr's own extraction pipeline is
// deterministic and the ordering bug lives elsewhere in the chain.
func TestExtract_DeterministicAcrossRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns `go run` several times; skipped with -short")
	}

	root := writeFixtureApp(t)

	const runs = 4
	var first string
	for i := 0; i < runs; i++ {
		e := newSeededExtractor(root) // fresh cache = fresh process simulation
		assets, err := e.ExtractModule(root)
		if err != nil {
			t.Fatalf("run %d: ExtractModule: %v", i, err)
		}
		if assets == nil {
			t.Fatalf("run %d: nil assets", i)
		}

		combined := "ROOT[" + assets.RootCSS + "] CSS[" + assets.CSS + "]"
		if i == 0 {
			first = combined
			t.Logf("baseline extraction: %s", first)
			continue
		}
		if combined != first {
			t.Fatalf("extraction output changed between runs (non-deterministic!)\n run 0: %s\n run %d: %s", first, i, combined)
		}
	}

	// The merge contract: packages combine sorted by import path
	// (config < modules/alpha < modules/beta < modules/zeta).
	e := newSeededExtractor(root)
	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatal(err)
	}

	iConfig := strings.Index(assets.CSS, "config")
	iAlpha := strings.Index(assets.CSS, "alpha")
	iBeta := strings.Index(assets.CSS, "beta")
	iZeta := strings.Index(assets.CSS, "zeta")

	if iConfig == -1 || iAlpha == -1 || iBeta == -1 || iZeta == -1 {
		t.Fatalf("missing one of config, alpha, beta, zeta in CSS: %q", assets.CSS)
	}

	if !(iConfig < iAlpha && iAlpha < iBeta && iBeta < iZeta) {
		t.Fatalf("merged CSS order broke the sorted-by-path contract: config < alpha < beta < zeta\n got CSS:\n%s", assets.CSS)
	}
}
