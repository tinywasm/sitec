//go:build !wasm

package ssr_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinywasm/assetmin"
	"github.com/tinywasm/modfind"
	"github.com/tinywasm/ssr"
)

// TestExtractAll_AppRootWinsOverFramework reproduces, end to end through the
// REAL extraction pipeline (codegen -> `go run` -> JSON round-trip -> the
// same assetmin wiring tinywasm/app uses), the contract documented in
// tinywasm/css/README.md's "SSR contract: RootCSS vs RenderCSS": RootCSS is a
// single-winner slot, so an application that exposes its own `RootCSS()`
// (its rebrand, e.g. config/css.go in a real app) must completely replace —
// not merge with — the framework's default catalog from a package whose
// import path ends in "tinywasm/css".
//
// This gap existed because ssr's own extraction tests (extract_test.go,
// extract_module_root_test.go) only ever check ONE module's RootCSS in
// isolation, and assetmin's own single-winner tests
// (tests/ssr_loader_root_override_test.go) hand-mock both providers via
// UpdateSSRModuleInSlot/RegisterComponents — neither exercises TWO real,
// conflicting RootCSS() producers going through ssr's actual compile-and-run
// extractor into assetmin's actual routeAssets/resolveAndApplyRootCSS
// resolution, which is the exact path a running app depends on.
func TestExtractAll_AppRootWinsOverFramework(t *testing.T) {
	base := t.TempDir()
	appDir := filepath.Join(base, "app")
	// The dir name must end in "tinywasm/css" — isFrameworkModule matches on
	// the MODULE PATH suffix, not the dir name, but this mirrors the real
	// layout (a checkout of github.com/tinywasm/css) for readability.
	cssDir := filepath.Join(base, "tinywasm-css")

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// The "framework" module: its import path ends in "tinywasm/css", the
	// literal suffix isFrameworkModule checks for. Its RootCSS declares the
	// default catalog — a brand token no app override touches
	// (--framework-only) plus the SAME token the app below overrides
	// (--color-accent), so the test can tell "app's value wins" apart from
	// "framework's other tokens silently vanish because the slot replaced,
	// not merged."
	write(filepath.Join(cssDir, "go.mod"), "module example.com/tinywasm/css\n\ngo 1.24\n")
	write(filepath.Join(cssDir, "css.go"), `//go:build !wasm

package css

type stylesheet string

func (s stylesheet) String() string { return string(s) }

func RootCSS() stylesheet {
	return stylesheet(":root{--framework-only:1;--color-accent:#e8a33d;}")
}
`)

	// The app: config/css.go is the rebrand — its own RootCSS(), built like
	// css.Theme() (default-shaped block, then an override block appended),
	// overriding --color-accent to a different value. Real hex values from
	// the bug report this guards, so a future regression is traceable back
	// to it.
	// The generated extractor main.go lives inside appDir (rootDir/.ssr_extract)
	// and imports BOTH discovered modules by path — it needs the app's own
	// go.mod to resolve "example.com/tinywasm/css" as buildable, exactly like
	// a real app's go.mod requires+replaces github.com/tinywasm/css.
	write(filepath.Join(appDir, "go.mod"), `module example.com/app

go 1.24

require example.com/tinywasm/css v0.0.0

replace example.com/tinywasm/css => ../tinywasm-css
`)
	write(filepath.Join(appDir, "main.go"), "package main\n\nimport _ \"example.com/tinywasm/css\"\n\nfunc main() {}\n")
	write(filepath.Join(appDir, "config", "css.go"), `//go:build !wasm

package config

type stylesheet string

func (s stylesheet) String() string { return string(s) }

func RootCSS() stylesheet {
	// Two blocks, like css.Theme()'s "default-shaped catalog, override tail
	// appended" — but --color-primary here is a value the framework fixture
	// below never mentions, so a later assertion that "#e8a33d" (the
	// framework's OWN literal) is gone from the served CSS cannot be
	// satisfied by accident just because this first block also happens to
	// exist; it only passes if the framework's block was truly dropped.
	return stylesheet(":root{--color-primary:#1b5d8c;}" +
		":root{--color-accent:#3f88bf;}")
}
`)

	e := ssr.New(appDir)
	e.SetLog(t.Log)
	f := modfind.New()
	f.Seed(appDir, []modfind.Module{
		{Path: "example.com/app", Dir: appDir},
		{Path: "example.com/tinywasm/css", Dir: cssDir},
	})
	e.SetFinder(f)

	all, err := e.ExtractAll()
	if err != nil {
		t.Fatalf("ExtractAll: %v", err)
	}

	// First, the extraction-layer contract: each module's own RootCSS comes
	// back labeled correctly, independent of assetmin's resolution.
	var sawRoot, sawFramework bool
	for _, a := range all {
		switch {
		case a.IsRoot:
			sawRoot = true
			if !strings.Contains(a.RootCSS, "#3f88bf") {
				t.Errorf("app module RootCSS missing its own override, got %q", a.RootCSS)
			}
		case a.IsFramework:
			sawFramework = true
			if !strings.Contains(a.RootCSS, "#e8a33d") {
				t.Errorf("framework module RootCSS missing its default, got %q", a.RootCSS)
			}
		}
	}
	if !sawRoot {
		t.Fatal("no module extracted with IsRoot=true — app/config/css.go was not recognized as the root")
	}
	if !sawFramework {
		t.Fatal("no module extracted with IsFramework=true — example.com/tinywasm/css was not recognized as the framework")
	}

	// Then the actual contract a running app depends on: wire the SAME
	// extractor into a real AssetMin exactly like tinywasm/app's
	// section-build.go does, and check what gets SERVED.
	am := assetmin.NewAssetMin(&assetmin.Config{OutputDir: t.TempDir()})
	am.SetSSRExtractor(e)
	am.LoadSSRModules()
	am.WaitForSSRLoad(10 * time.Second)

	served, err := am.GetMinifiedCSS()
	if err != nil {
		t.Fatalf("GetMinifiedCSS: %v", err)
	}
	css := string(served)

	if !strings.Contains(css, "#3f88bf") {
		t.Errorf("served CSS does not contain the app's RootCSS override.\n  got: %s", css)
	}
	if strings.Contains(css, "#e8a33d") {
		t.Errorf("served CSS still contains the framework's default --color-accent — "+
			"RootCSS is a single-winner slot, the app's RootCSS() must fully replace it, not merge.\n  got: %s", css)
	}
	if strings.Contains(css, "framework-only") {
		t.Errorf("served CSS still contains a framework-only token the app's RootCSS() "+
			"never declared — the app's RootCSS() must fully replace the framework's, not merge with it.\n  got: %s", css)
	}
}
