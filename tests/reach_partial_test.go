//go:build !wasm

package sitec_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tinywasm/modfind"
	"github.com/tinywasm/sitec"
)

// TestExtractAll_PartialReachabilityDoesNotFilter is the regression test for
// the bug found live in misitio's TUI: two consecutive scans reported 18 and
// 16 "skipped" packages for the same project — a couple of packages that ARE
// reachable were reported as unreachable on the first (cold module cache)
// scan, because computeReachability silently used whichever of its two `go
// list -deps` probes (native, wasm) happened to succeed, discarding the
// other's failure with no signal.
//
// A package wrongly marked "unreachable" is not just a noisy log line — it
// gets FILTERED OUT of the SSR extraction, so its styles genuinely don't
// ship on that scan. The fix: when even one probe fails, the union is
// INCOMPLETE, and modulesToAliases must not filter on it at all — better to
// keep an actually-unreachable package around for one extra scan than to
// silently drop a reachable one's styles.
//
// This test forces exactly that: a package ("widget") that a *complete*
// reachability computation would correctly mark unreachable (nothing in the
// app imports it), with one of the two GraphLister probes made to fail. It
// asserts the package's CSS still ships — filtering was skipped, not that it
// "got lucky" and was found reachable.
func TestExtractAll_PartialReachabilityDoesNotFilter(t *testing.T) {
	appDir, widgetDir := setupReachFixture(t)

	e := sitec.New(appDir)
	e.SetLog(t.Log)
	f := modfind.New()
	f.Seed(appDir, []modfind.Module{
		{Path: "example.com/app", Dir: appDir},
		{Path: "example.com/widget", Dir: widgetDir},
	})
	e.SetFinder(f)

	// One target succeeds and correctly reports the widget as NOT in its
	// deps (nothing imports it) — exactly what a real `go list` would say.
	// The other target fails outright, simulating a cold module cache.
	e.SetGraphLister(func(rootDir, pattern, goos, goarch string) ([]string, error) {
		if goarch == "wasm" {
			return nil, errors.New("simulated cold module cache: go list failed")
		}
		return []string{"example.com/app"}, nil // "widget" deliberately absent
	})

	all, err := e.ExtractAll()
	if err != nil {
		t.Fatalf("ExtractAll: %v", err)
	}

	var sawWidget bool
	for _, a := range all {
		if a.ModuleName == "example.com/widget" {
			sawWidget = true
		}
	}
	if !sawWidget {
		t.Fatal("widget module was filtered out despite a partial (one probe failed) reachability " +
			"computation — its CSS would silently not ship")
	}
}

// TestExtractAll_CompleteReachabilityStillFilters is the companion case:
// when BOTH probes succeed and agree the widget is unreachable, it must
// still be filtered — the partial-failure fix must not turn off filtering
// altogether.
func TestExtractAll_CompleteReachabilityStillFilters(t *testing.T) {
	appDir, widgetDir := setupReachFixture(t)

	// The app needs its own asset-declaring package: with widget correctly
	// filtered out, an app with zero producers would leave nothing to
	// extract at all (a different, expected error) — this fixture asserts
	// widget's specific absence from a non-empty result, not just "extraction
	// produced nothing".
	if err := os.MkdirAll(filepath.Join(appDir, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config", "css.go"), []byte(`//go:build !wasm

package config

type stylesheet string

func (s stylesheet) String() string { return string(s) }

func RootCSS() stylesheet { return ":root{--app:1}" }
`), 0644); err != nil {
		t.Fatal(err)
	}

	e := sitec.New(appDir)
	e.SetLog(t.Log)
	f := modfind.New()
	f.Seed(appDir, []modfind.Module{
		{Path: "example.com/app", Dir: appDir},
		{Path: "example.com/widget", Dir: widgetDir},
	})
	e.SetFinder(f)

	e.SetGraphLister(func(rootDir, pattern, goos, goarch string) ([]string, error) {
		// "example.com/app/config" (not just the bare module path) is the
		// actual package import path expandToSSRPackages checks against —
		// widget is deliberately absent from both.
		return []string{"example.com/app", "example.com/app/config"}, nil
	})

	all, err := e.ExtractAll()
	if err != nil {
		t.Fatalf("ExtractAll: %v", err)
	}

	var sawApp bool
	for _, a := range all {
		if a.ModuleName == "example.com/widget" {
			t.Fatal("widget module was NOT filtered despite complete reachability data agreeing it's unreachable")
		}
		if a.IsRoot {
			sawApp = true
		}
	}
	if !sawApp {
		t.Fatal("app module (the root) was not extracted at all")
	}
}

func setupReachFixture(t *testing.T) (appDir, widgetDir string) {
	t.Helper()
	base := t.TempDir()
	appDir = filepath.Join(base, "app")
	widgetDir = filepath.Join(base, "widget")

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write(filepath.Join(appDir, "go.mod"), `module example.com/app

go 1.24

require example.com/widget v0.0.0

replace example.com/widget => ../widget
`)
	write(filepath.Join(appDir, "main.go"), "package main\n\nfunc main() {}\n")

	// widget declares CSS but nothing in appDir imports it — a REAL, complete
	// reachability computation would correctly mark it unreachable too. The
	// two tests differ only in whether the simulated computation is partial.
	write(filepath.Join(widgetDir, "go.mod"), "module example.com/widget\n\ngo 1.24\n")
	write(filepath.Join(widgetDir, "css.go"), `//go:build !wasm

package widget

type stylesheet string

func (s stylesheet) String() string { return string(s) }

type Widget struct{}

func (w *Widget) RenderCSS() stylesheet { return ".widget{color:red}" }
`)

	return appDir, widgetDir
}
