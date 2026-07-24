//go:build !wasm

package ssr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tinywasm/modfind"
	"github.com/tinywasm/ssr"
)

// writeFixtureApp creates a realistic consumer app layout:
//
//	root/
//	  go.mod            (module example.com/app)
//	  config/css.go     (RootCSS + RenderCSS)
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

	write("go.mod", "module example.com/app\n\ngo 1.24\n")

	write("config/css.go", `//go:build !wasm

package config

type stylesheet string

func (s stylesheet) String() string { return string(s) }

type Theme struct{}

func (t *Theme) RootCSS() stylesheet   { return stylesheet(":root{--brand:#00ADD8}") }
func (t *Theme) RenderCSS() stylesheet { return stylesheet(".config{order:0}") }
`)

	componentCSS := func(pkg, rule string) string {
		return `//go:build !wasm

package ` + pkg + `

type stylesheet string

func (s stylesheet) String() string { return string(s) }

type Component struct{}

func (c *Component) RenderCSS() stylesheet { return stylesheet("` + rule + `") }
`
	}

	write("modules/alpha/css.go", componentCSS("alpha", ".alpha{order:1}"))
	write("modules/beta/css.go", componentCSS("beta", ".beta{color:blue}"))
	write("modules/zeta/css.go", componentCSS("zeta", ".zeta{order:3}"))

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
	const wantCSS = ".config{order:0}.alpha{order:1}.beta{color:blue}.zeta{order:3}"
	e := newSeededExtractor(root)
	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatal(err)
	}
	if assets.CSS != wantCSS {
		t.Fatalf("merged CSS order broke the sorted-by-path contract\n want: %q\n got:  %q", wantCSS, assets.CSS)
	}
}
