//go:build !wasm

package sitec_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinywasm/modfind"
	"github.com/tinywasm/sitec"
)

// TestExtractAll_NestedSiteDirectoryIsRoot reproduces a real gap surfaced
// while diagnosing a CI/CD requirement: build several similarly-themed sites
// from ONE shared module, each site living in its own subdirectory
// (sites/a/, sites/b/) with no go.mod of its own, differing only in its own
// config/css.go override — the natural shape for a pipeline that runs `sitec
// build` once per site directory.
//
// Before this fix, IsRoot came from isRootDir(m.dir, e.rootDir): m.dir is
// always a MODULE's root directory (from `go list -m`), so the comparison
// only ever matched when sitec was started exactly at that root. Starting at
// a nested site subdirectory made it false unconditionally, so
// routeAssets rejected the site's own RootCSS() (neither root nor
// framework) and every site silently fell back to the framework's raw
// defaults — indistinguishable from one another, exactly what a multi-site
// build must not do.
func TestExtractAll_NestedSiteDirectoryIsRoot(t *testing.T) {
	base := t.TempDir()
	appDir := filepath.Join(base, "app") // the ONE shared module for every site
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

	write(filepath.Join(cssDir, "go.mod"), "module example.com/tinywasm/css\n\ngo 1.24\n")
	write(filepath.Join(cssDir, "css.go"), `//go:build !wasm

package css

type stylesheet string

func (s stylesheet) String() string { return string(s) }

func RootCSS() stylesheet { return stylesheet(":root{--framework-default:1;}") }
`)

	write(filepath.Join(appDir, "go.mod"), `module example.com/app

go 1.24

require example.com/tinywasm/css v0.0.0

replace example.com/tinywasm/css => ../tinywasm-css
`)

	write(filepath.Join(appDir, "sites", "a", "config", "css.go"), `//go:build !wasm

package config

type stylesheet string

func (s stylesheet) String() string { return string(s) }

func RootCSS() stylesheet { return stylesheet(":root{--brand:siteA;}") }
`)
	write(filepath.Join(appDir, "sites", "a", "main.go"), `package main

import (
	_ "example.com/app/sites/a/config"
	_ "example.com/tinywasm/css"
)

func main() {}
`)

	write(filepath.Join(appDir, "sites", "b", "config", "css.go"), `//go:build !wasm

package config

type stylesheet string

func (s stylesheet) String() string { return string(s) }

func RootCSS() stylesheet { return stylesheet(":root{--brand:siteB;}") }
`)
	write(filepath.Join(appDir, "sites", "b", "main.go"), `package main

import (
	_ "example.com/app/sites/b/config"
	_ "example.com/tinywasm/css"
)

func main() {}
`)

	check := func(t *testing.T, siteDir, wantBrand, dontWantBrand string) {
		t.Helper()

		e := sitec.New(siteDir)
		e.SetLog(t.Log)
		f := modfind.New()
		f.Seed(siteDir, []modfind.Module{
			{Path: "example.com/app", Dir: appDir},
			{Path: "example.com/tinywasm/css", Dir: cssDir},
		})
		e.SetFinder(f)

		all, err := e.ExtractAll()
		if err != nil {
			t.Fatalf("ExtractAll: %v", err)
		}

		var sawRoot bool
		for _, a := range all {
			if a.ModuleName != "example.com/app" {
				continue
			}
			sawRoot = a.IsRoot
			if !a.IsRoot {
				t.Errorf("example.com/app extracted with IsRoot=false when started from %s — "+
					"the shared module that owns this site subdirectory must be recognized as root", siteDir)
			}
			if !strings.Contains(a.RootCSS, wantBrand) {
				t.Errorf("RootCSS missing %q, got %q", wantBrand, a.RootCSS)
			}
			if strings.Contains(a.RootCSS, dontWantBrand) {
				t.Errorf("RootCSS leaked the sibling site's brand %q, got %q", dontWantBrand, a.RootCSS)
			}
		}
		if !sawRoot {
			t.Fatal("example.com/app was not extracted with IsRoot=true")
		}

		// The contract a CI/CD build actually depends on: what gets served,
		// through the same routing a real `sitec build`/app dev server uses.
		am := sitec.NewAssetMin(&sitec.Config{OutputDir: t.TempDir()})
		am.SetSSRExtractor(e)
		am.LoadSSRModules()
		am.WaitForSSRLoad(10 * time.Second)

		served, err := am.GetMinifiedCSS()
		if err != nil {
			t.Fatalf("GetMinifiedCSS: %v", err)
		}
		css := string(served)
		if !strings.Contains(css, wantBrand) {
			t.Errorf("served CSS missing this site's own brand %q.\n  got: %s", wantBrand, css)
		}
		if strings.Contains(css, dontWantBrand) {
			t.Errorf("served CSS contains the sibling site's brand %q instead of this site's own.\n  got: %s", dontWantBrand, css)
		}
	}

	t.Run("site_a", func(t *testing.T) {
		check(t, filepath.Join(appDir, "sites", "a"), "siteA", "siteB")
	})
	t.Run("site_b", func(t *testing.T) {
		check(t, filepath.Join(appDir, "sites", "b"), "siteB", "siteA")
	})
}
