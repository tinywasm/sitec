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

// writeTree writes a set of relative path -> content pairs under base.
func writeTree(t *testing.T, base string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(base, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

// setupUnreachablePackageProject builds the exact shape that breaks tinywasm/layout
// against components@v0.5.7:
//
//	app          -> requires dep, imports ONLY dep/used
//	dep/used     -> declares RenderCSS, imports nothing extra   (reachable)
//	dep/slider   -> declares RenderCSS, imports example.com/extra (NOT reachable from app)
//
// example.com/extra is absent from app's go.mod/go.sum, exactly like
// github.com/tinywasm/date is absent from layout's go.sum: the consumer never
// reaches the package that needs it, so `go mod tidy` will never add it.
func setupUnreachablePackageProject(t *testing.T) (appDir, depDir string, finder *modfind.Finder) {
	t.Helper()
	base := t.TempDir()
	appDir = filepath.Join(base, "app")
	depDir = filepath.Join(base, "dep")

	writeTree(t, depDir, map[string]string{
		"go.mod": "module example.com/dep\n\ngo 1.24\n",
		"used/css.go": `package used
` + stylesheetHelper + `
type Used struct{}

func (u *Used) RenderCSS() stylesheet { return ".used{color:red}" }
`,
		// The unreachable package. Its import of example.com/extra cannot be
		// resolved from app's module graph.
		"slider/css.go": `package slider

import _ "example.com/extra"
` + stylesheetHelper + `
type Slider struct{}

func (s *Slider) RenderCSS() stylesheet { return ".slider{color:blue}" }
`,
	})

	writeTree(t, appDir, map[string]string{
		"go.mod": `module example.com/app

go 1.24

require example.com/dep v0.0.0

replace example.com/dep => ../dep
`,
		"main.go": `package main

import _ "example.com/dep/used"

func main() {}
`,
		"config/css.go": `package config
` + stylesheetHelper + `
type App struct{}

func (a *App) RenderCSS() stylesheet { return ".app{color:green}" }
`,
	})

	finder = modfind.New()
	finder.Seed(appDir, []modfind.Module{
		{Path: "example.com/app", Dir: appDir, IsMain: true},
		{Path: "example.com/dep", Dir: depDir},
	})
	return appDir, depDir, finder
}

// TestExtractAll_UnreachablePackageDoesNotKillExtraction is the regression test for
// the layout/platformd failure:
//
//	missing go.sum entry for module providing package github.com/tinywasm/date
//	(imported by github.com/tinywasm/components/calendarslider)
//
// The consumer never imports calendarslider. ssr pulls it into the generated
// extractor main.go anyway (expandToSSRPackages walks every directory of every
// discovered module), so `go run` fails to build and EVERY stylesheet is lost.
//
// The extractor must skip packages that the consumer's build graph cannot reach.
func TestExtractAll_UnreachablePackageDoesNotKillExtraction(t *testing.T) {
	appDir, _, finder := setupUnreachablePackageProject(t)

	e := sitec.New(appDir)
	e.SetLog(t.Log)
	e.SetFinder(finder)

	all, err := e.ExtractAll()
	if err != nil {
		t.Fatalf("ExtractAll: %v", err)
	}

	var css strings.Builder
	for _, a := range all {
		css.WriteString(a.RootCSS)
		css.WriteString(a.CSS)
	}
	got := css.String()

	if !strings.Contains(got, ".app{color:green}") {
		t.Errorf("the app's own CSS was lost because an unreachable dependency package\n"+
			"failed to compile in the generated extractor.\n  got: %q", got)
	}
	if !strings.Contains(got, ".used{color:red}") {
		t.Errorf("the reachable dependency package's CSS was lost.\n  got: %q", got)
	}
	if strings.Contains(got, ".slider{color:blue}") {
		t.Errorf("ssr extracted CSS from a package the consumer never imports; "+
			"unreachable packages must be skipped.\n  got: %q", got)
	}
}

// TestExtractAll_ReportsFailureInsteadOfSilentlyReturningNothing covers the second
// half of the defect: extractAssetsForModule fails identically for every module,
// but ExtractAll logs the error per module and returns (nil, nil). assetmin reads
// that as success, so the app serves an EMPTY stylesheet and still prints "build ok".
//
// The log noise (one identical line per module, ~20 in layout) is the same bug seen
// from the terminal: one root failure must be reported once, and it must reach the
// caller as an error.
func TestExtractAll_ReportsFailureInsteadOfSilentlyReturningNothing(t *testing.T) {
	appDir, depDir, finder := setupUnreachablePackageProject(t)

	// Only the main module is broken here: remove the unresolvable dependency
	// package so this test isolates the "report once and propagate" contract.
	if err := os.RemoveAll(filepath.Join(depDir, "slider")); err != nil {
		t.Fatal(err)
	}

	// A broken package in the MAIN module must always fail loudly — it is the
	// developer's own code, not an unused dependency package. This survives the
	// skip-unresolvable filter by design.
	writeTree(t, appDir, map[string]string{
		"config/css.go": `package config
` + stylesheetHelper + `
type App struct{}

func (a *App) RenderCSS() stylesheet { return thisSymbolDoesNotExist }
`,
	})

	var logged []string
	e := sitec.New(appDir)
	e.SetLog(func(a ...any) {
		var line strings.Builder
		for _, v := range a {
			line.WriteString(strings.TrimSpace(sprint(v)))
			line.WriteString(" ")
		}
		logged = append(logged, strings.TrimSpace(line.String()))
		t.Log(strings.TrimSpace(line.String()))
	})
	e.SetFinder(finder)

	all, err := e.ExtractAll()
	if err == nil {
		t.Errorf("ExtractAll returned no error even though extraction produced nothing; "+
			"assetmin treats this as success and the app ships an empty stylesheet (got %d asset sets)", len(all))
	}

	var extractErrors int
	for _, l := range logged {
		if strings.Contains(l, "ssr extract error") {
			extractErrors++
		}
	}
	if extractErrors > 1 {
		t.Errorf("one root failure was logged %d times, once per module; it must be reported once", extractErrors)
	}
}

// TestExtractAll_NoAssetLibrariesWarnedOnce: the warning fires on every ExtractAll /
// ExtractModule call, i.e. on every file save in the watcher. No production caller
// ever calls SetAssetLibraries, so this is unconditional noise on every keystroke.
func TestExtractAll_NoAssetLibrariesWarnedOnce(t *testing.T) {
	appDir, _, finder := setupUnreachablePackageProject(t)

	var warnings int
	e := sitec.New(appDir)
	e.SetLog(func(a ...any) {
		for _, v := range a {
			if strings.Contains(sprint(v), "no asset libraries configured") {
				warnings++
			}
		}
	})
	e.SetFinder(finder)

	for i := 0; i < 3; i++ {
		_, _ = e.ExtractAll()
	}

	if warnings > 1 {
		t.Errorf("the 'no asset libraries configured' warning was logged %d times across 3 extractions; "+
			"it must be emitted at most once per extractor", warnings)
	}
}

func sprint(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}

func TestAnchor_StartedSubdirDoesNotLeakSiblingPackages(t *testing.T) {
	base := t.TempDir()
	appDir := filepath.Join(base, "app")
	writeTree(t, appDir, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.24\n",
		"main.go": "package main\n\nfunc main() {}\n",
		"alpha/css.go": `package alpha
` + stylesheetHelper + `
type Alpha struct{}
func (a *Alpha) RenderCSS() stylesheet { return ".alpha{color:red}" }
`,
		"beta/css.go": `package beta
` + stylesheetHelper + `
type Beta struct{}
func (b *Beta) RenderCSS() stylesheet { return ".beta{color:blue}" }
`,
	})

	e := sitec.New(filepath.Join(appDir, "alpha"))
	e.SetLog(t.Log)
	f := modfind.New()
	f.Seed(appDir, []modfind.Module{{Path: "example.com/app", Dir: appDir, IsMain: true}})
	e.SetFinder(f)

	a, err := e.ExtractModule(filepath.Join(appDir, "alpha"))
	if err != nil {
		t.Fatalf("ExtractModule: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil assets")
	}

	if !strings.Contains(a.CSS, ".alpha{color:red}") {
		t.Errorf("expected alpha's CSS, got %q", a.CSS)
	}
	if strings.Contains(a.CSS, ".beta{color:blue}") {
		t.Errorf("LEAKED sibling package CSS not reachable from started dir: %q", a.CSS)
	}
}
