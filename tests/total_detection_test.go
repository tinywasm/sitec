//go:build !wasm

package ssr_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinywasm/modfind"
	"github.com/tinywasm/ssr"
)

func writeAppFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func setupBaseApp(t *testing.T) string {
	root := t.TempDir()
	writeAppFile(t, root, "go.mod", "module example.com/app\n\ngo 1.25.2\n")
	return root
}

func seedExtractor(root string) *ssr.Extractor {
	e := ssr.New(root)
	f := modfind.New()
	f.Seed(root, []modfind.Module{{Path: "example.com/app", Dir: root, IsMain: true}})
	e.SetFinder(f)
	return e
}

const stylesheetHelper = `
type stylesheet string
func (s stylesheet) String() string { return string(s) }
`

// TestTwoProducersOnePackage: a package declaring RenderCSS on Alpha and Beta emits both stylesheets, in type-name order
func TestTwoProducersOnePackage(t *testing.T) {
	root := setupBaseApp(t)
	writeAppFile(t, root, "components/css.go", `package components
`+stylesheetHelper+`
type Alpha struct{}
func (a *Alpha) RenderCSS() stylesheet { return ".alpha{color:red}" }

type Beta struct{}
func (b *Beta) RenderCSS() stylesheet { return ".beta{color:blue}" }
`)

	e := seedExtractor(root)
	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatal(err)
	}
	if assets == nil {
		t.Fatal("expected non-nil assets")
	}

	want := ".alpha{color:red}.beta{color:blue}"
	if assets.CSS != want {
		t.Fatalf("expected CSS %q, got %q", want, assets.CSS)
	}
}

// TestProducerOutsideCssGo: a RenderCSS in masterdetail.go, with no css.go, is collected
func TestProducerOutsideCssGo(t *testing.T) {
	root := setupBaseApp(t)
	writeAppFile(t, root, "components/masterdetail.go", `package components
`+stylesheetHelper+`
type MasterDetail struct{}
func (m *MasterDetail) RenderCSS() stylesheet { return ".md{color:green}" }
`)

	e := seedExtractor(root)
	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatal(err)
	}
	if assets == nil {
		t.Fatal("expected non-nil assets")
	}

	want := ".md{color:green}"
	if assets.CSS != want {
		t.Fatalf("expected CSS %q, got %q", want, assets.CSS)
	}
}

// TestNoProducerIsAnError: a package importing widget/style and declaring none fails the build, naming the package
func TestNoProducerIsAnError(t *testing.T) {
	root := setupBaseApp(t)
	writeAppFile(t, root, "components/unrelated.go", `package components

import _ "github.com/tinywasm/widget/style"
`)

	e := seedExtractor(root)
	_, err := e.ExtractModule(root)
	if err == nil {
		t.Fatal("expected build failure when package imports asset library and declares no producer")
	}

	expectedStr := "ssr: package example.com/app/components imports github.com/tinywasm/widget/style but declares no producer"
	if !strings.Contains(err.Error(), expectedStr) {
		t.Fatalf("expected error to contain %q, got %v", expectedStr, err)
	}
}

// TestProducerMultilineSignature: a receiver split across lines is detected
func TestProducerMultilineSignature(t *testing.T) {
	root := setupBaseApp(t)
	writeAppFile(t, root, "components/css.go", `package components
`+stylesheetHelper+`
type MultiLine struct{}

func (
	m *MultiLine,
) RenderCSS() stylesheet {
	return ".multiline{color:yellow}"
}
`)

	e := seedExtractor(root)
	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatal(err)
	}
	if assets == nil {
		t.Fatal("expected non-nil assets")
	}

	want := ".multiline{color:yellow}"
	if assets.CSS != want {
		t.Fatalf("expected CSS %q, got %q", want, assets.CSS)
	}
}

// TestProducerGenericReceiver: *Table[T] is detected, and either collected or reported — never skipped silently
func TestProducerGenericReceiver(t *testing.T) {
	root := setupBaseApp(t)
	writeAppFile(t, root, "components/css.go", `package components
`+stylesheetHelper+`
type Table[T any] struct{}
func (t *Table[T]) RenderCSS() stylesheet { return ".table{color:purple}" }
`)

	e := seedExtractor(root)
	// Because Table[T] is generic, instantiating it as &Table{} in generated main.go will fail to compile.
	// We want to ensure it is reported as unsupported / fails compilation (never skipped silently).
	_, err := e.ExtractModule(root)
	if err == nil {
		t.Fatal("expected compilation failure for generic receiver")
	}
}

// TestSingleLayerStatement: merged output has exactly one @layer …;, before any rule
func TestSingleLayerStatement(t *testing.T) {
	root := setupBaseApp(t)
	writeAppFile(t, root, "components/a/css.go", `package a
`+stylesheetHelper+`
type A struct{}
func (a *A) RenderCSS() stylesheet { return "@layer base, components;\n@layer components { .a { color: red; } }" }
`)
	writeAppFile(t, root, "components/b/css.go", `package b
`+stylesheetHelper+`
type B struct{}
func (b *B) RenderCSS() stylesheet { return "@layer base, components;\n@layer components { .b { color: blue; } }" }
`)

	e := seedExtractor(root)
	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatal(err)
	}
	if assets == nil {
		t.Fatal("expected non-nil assets")
	}

	// Should have exactly one layer statement at the very top, and then the merged blocks.
	// Since identical blocks are merged, .a and .b are in components layer but not byte-identical,
	// so they are not merged into one rule, but they are in the same @layer components block.
	want := "@layer base, components;\n@layer components {\n.a { color: red; }\n.b { color: blue; }\n}\n"
	if assets.CSS != want {
		t.Fatalf("expected CSS:\n%q\ngot:\n%q", want, assets.CSS)
	}
}

// TestConflictingLayerOrderErrors: two packages with different layer orders is an error, not last-one-wins
func TestConflictingLayerOrderErrors(t *testing.T) {
	root := setupBaseApp(t)
	writeAppFile(t, root, "components/a/css.go", `package a
`+stylesheetHelper+`
type A struct{}
func (a *A) RenderCSS() stylesheet { return "@layer base, components;" }
`)
	writeAppFile(t, root, "components/b/css.go", `package b
`+stylesheetHelper+`
type B struct{}
func (b *B) RenderCSS() stylesheet { return "@layer components, base;" }
`)

	e := seedExtractor(root)
	_, err := e.ExtractModule(root)
	if err == nil {
		t.Fatal("expected error due to conflicting @layer order")
	}

	expectedStr := "ssr: conflicting @layer order:"
	if !strings.Contains(err.Error(), expectedStr) {
		t.Fatalf("expected error to contain %q, got %v", expectedStr, err)
	}
}

// TestIdenticalBlocksMerged: two components using the same primitive emit one rule with both selectors
func TestIdenticalBlocksMerged(t *testing.T) {
	root := setupBaseApp(t)
	writeAppFile(t, root, "components/a/css.go", `package a
`+stylesheetHelper+`
type A struct{}
func (a *A) RenderCSS() stylesheet { return "@layer components { .alpha { display: flex; } }" }
`)
	writeAppFile(t, root, "components/b/css.go", `package b
`+stylesheetHelper+`
type B struct{}
func (b *B) RenderCSS() stylesheet { return "@layer components { .beta { display: flex; } }" }
`)

	e := seedExtractor(root)
	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatal(err)
	}

	want := "@layer components {\n.alpha, .beta { display: flex; }\n}\n"
	if assets.CSS != want {
		t.Fatalf("expected CSS:\n%q\ngot:\n%q", want, assets.CSS)
	}
}

// TestMergeStopsAtOverlap: counter-fixture: an intervening rule targeting an overlapping selector prevents the merge
func TestMergeStopsAtOverlap(t *testing.T) {
	root := setupBaseApp(t)
	// We have three rules in components layer:
	// 1. .alpha { display: flex; }
	// 2. .alpha { color: red; }  <-- Intervening rule targeting .alpha
	// 3. .beta { display: flex; }
	// Since rule 2 targets .alpha (which is in rule 1), rule 1 and rule 3 must NOT merge!
	writeAppFile(t, root, "components/a/css.go", `package a
`+stylesheetHelper+`
type A struct{}
func (a *A) RenderCSS() stylesheet { return "@layer components { .alpha { display: flex; } }" }
`)
	writeAppFile(t, root, "components/b/css.go", `package b
`+stylesheetHelper+`
type B struct{}
func (b *B) RenderCSS() stylesheet { return "@layer components { .alpha { color: red; } }" }
`)
	writeAppFile(t, root, "components/c/css.go", `package c
`+stylesheetHelper+`
type C struct{}
func (c *C) RenderCSS() stylesheet { return "@layer components { .beta { display: flex; } }" }
`)

	e := seedExtractor(root)
	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatal(err)
	}

	want := "@layer components {\n.alpha { display: flex; }\n.alpha { color: red; }\n.beta { display: flex; }\n}\n"
	if assets.CSS != want {
		t.Fatalf("expected CSS:\n%q\ngot:\n%q", want, assets.CSS)
	}
}

// TestPanicNamesProducer: a producer that panics fails the run with a message naming its package and receiver type, not a generated-code stack
func TestPanicNamesProducer(t *testing.T) {
	root := setupBaseApp(t)
	writeAppFile(t, root, "components/css.go", `package components
`+stylesheetHelper+`
type BadComponent struct{}
func (b *BadComponent) RenderCSS() stylesheet { panic("ouch") }
`)

	e := seedExtractor(root)
	_, err := e.ExtractModule(root)
	if err == nil {
		t.Fatal("expected failure due to panic")
	}

	expectedStr := "ssr: producer panic in package example.com/app/components, type BadComponent: ouch"
	if !strings.Contains(err.Error(), expectedStr) {
		t.Fatalf("expected error message to contain: %q, but got: %v", expectedStr, err)
	}
}

// TestZeroValueProducer: a producer whose output would differ if a field were read still emits the zero-value form
func TestZeroValueProducer(t *testing.T) {
	root := setupBaseApp(t)
	writeAppFile(t, root, "components/css.go", `package components
`+stylesheetHelper+`
type MyComponent struct {
	Color string
}
func (m *MyComponent) RenderCSS() stylesheet {
	if m.Color == "" {
		return ".zero{color:black}"
	}
	return stylesheet(".notzero{color:" + m.Color + "}")
}
`)

	e := seedExtractor(root)
	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatal(err)
	}

	want := ".zero{color:black}"
	if assets.CSS != want {
		t.Fatalf("expected zero-value CSS:\n%q\ngot:\n%q", want, assets.CSS)
	}
}
