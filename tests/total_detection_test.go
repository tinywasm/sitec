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

func seedExtractor(root string) *sitec.Extractor {
	e := sitec.New(root)
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
	e.SetAssetLibraries([]string{"github.com/tinywasm/widget/style"})
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
	_, err := e.ExtractModule(root)
	if err == nil {
		t.Fatal("expected compilation failure for generic receiver")
	}

	expectedStr := "ssr: package example.com/app/components declares producer RenderCSS() on generic type Table[…]; generic receivers cannot be instantiated as a zero value — use a concrete type"
	if !strings.Contains(err.Error(), expectedStr) {
		t.Fatalf("expected error message to contain:\n%q\ngot:\n%q", expectedStr, err.Error())
	}
}

// TestEmptyAssetLibrariesIsLogged: an extraction with no configured libraries emits the warning through SetLog; with a list configured it does not
func TestEmptyAssetLibrariesIsLogged(t *testing.T) {
	root := setupBaseApp(t)
	writeAppFile(t, root, "components/css.go", `package components
`+stylesheetHelper+`
type Alpha struct{}
func (a *Alpha) RenderCSS() stylesheet { return ".alpha{color:red}" }
`)

	// 1. Empty AssetLibraries: warning should be logged
	var logs []string
	e1 := seedExtractor(root)
	e1.SetLog(func(args ...any) {
		var parts []string
		for _, arg := range args {
			if s, ok := arg.(string); ok {
				parts = append(parts, s)
			}
		}
		logs = append(logs, strings.Join(parts, " "))
	})

	_, err := e1.ExtractModule(root)
	if err != nil {
		t.Fatal(err)
	}

	warningMsg := "ssr: no asset libraries configured; packages that import a styling library and declare no producer will NOT fail the build (see SetAssetLibraries)"
	found := false
	for _, l := range logs {
		if strings.Contains(l, warningMsg) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected warning %q in logs, got: %v", warningMsg, logs)
	}

	// 2. Non-empty AssetLibraries: warning should NOT be logged
	logs = nil
	e2 := seedExtractor(root)
	e2.SetAssetLibraries([]string{"github.com/tinywasm/widget/style"})
	e2.SetLog(func(args ...any) {
		var parts []string
		for _, arg := range args {
			if s, ok := arg.(string); ok {
				parts = append(parts, s)
			}
		}
		logs = append(logs, strings.Join(parts, " "))
	})

	_, err = e2.ExtractModule(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, l := range logs {
		if strings.Contains(l, warningMsg) {
			t.Fatalf("did not expect warning %q when AssetLibraries is set, but got in logs: %v", warningMsg, logs)
		}
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
