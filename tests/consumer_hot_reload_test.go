//go:build !wasm

package ssr_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinywasm/assetmin"
)

// This file reproduces the exact wiring a consumer application (godev/tinywasm
// CLI) uses in dev mode:
//
//	extractor := ssr.New(root)
//	am := assetmin.NewAssetMin(cfg)
//	am.SetSSRExtractor(extractor)
//	am.LoadSSRModules()              // initial async load at startup
//	... file watcher fires ...
//	am.ReloadSSRModule(changedDir)   // hot reload on css.go edit
//
// The symptom under investigation: after editing a component's css.go the
// rendered styles are wrong (old rules win) until the whole consumer app is
// restarted.

func newConsumerStack(t *testing.T, root string) *assetmin.AssetMin {
	t.Helper()
	extractor := newSeededExtractor(root)
	am := assetmin.NewAssetMin(&assetmin.Config{
		OutputDir: filepath.Join(t.TempDir(), "public"),
		RootDir:   root,
	})
	am.SetLog(t.Log)
	am.SetSSRExtractor(extractor)
	return am
}

func cssBody(t *testing.T, am *assetmin.AssetMin) string {
	t.Helper()
	css, err := am.GetMinifiedCSS()
	if err != nil {
		t.Fatalf("GetMinifiedCSS: %v", err)
	}
	return string(css)
}

// TestConsumerHotReload_SubpackageEdit reproduces the dev-mode flow: startup
// load, then a watcher-driven reload after editing modules/beta/css.go.
//
// Expected behavior: style.css contains exactly one .beta rule with the NEW
// color, in the same position the old one had.
func TestConsumerHotReload_SubpackageEdit(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns `go run`; skipped with -short")
	}

	root := writeFixtureApp(t)
	am := newConsumerStack(t, root)

	// 1) Startup: initial async SSR load (what the consumer does at boot).
	am.LoadSSRModules()
	am.WaitForSSRLoad(60 * time.Second)

	boot := cssBody(t, am)
	t.Logf("style.css after startup:\n%s", boot)
	if !strings.Contains(boot, "beta") {
		t.Fatalf("startup style.css missing beta rule:\n%s", boot)
	}

	// 2) Developer edits modules/beta/css.go: "beta" -> "beta-edited".
	betaDir := filepath.Join(root, "modules", "beta")
	edited := strings.ReplaceAll(mustRead(t, filepath.Join(betaDir, "css.go")), `"beta"`, `"beta-edited"`)
	if err := os.WriteFile(filepath.Join(betaDir, "css.go"), []byte(edited), 0644); err != nil {
		t.Fatal(err)
	}

	// 3) The file watcher (assetmin.SSRFileWatcher) routes the css.go event as:
	//    ReloadSSRModule(filepath.Dir(changedFile))
	if err := am.ReloadSSRModule(betaDir); err != nil {
		t.Fatalf("ReloadSSRModule: %v", err)
	}

	after := cssBody(t, am)
	t.Logf("style.css after hot reload:\n%s", after)

	// The old rule must be gone...
	if strings.Contains(after, "beta{") {
		t.Errorf("STALE RULE: old 'beta' still present after hot reload — the browser keeps rendering the outdated style until the app restarts")
	}
	// ...the new one present...
	if !strings.Contains(after, "beta-edited") {
		t.Errorf("new 'beta-edited' missing after hot reload")
	}

	// Cascade check: with equal specificity the LAST rule wins in CSS. If a
	// stale duplicate sits after the fresh rule, the browser renders the old
	// style even though the new one is present in the file.
	if iNew, iOld := strings.Index(after, "beta-edited"), strings.Index(after, "beta{"); iNew != -1 && iOld != -1 && iOld > iNew {
		t.Errorf("CASCADE BUG: stale rule appears AFTER the new rule, so the old style wins in the browser")
	}
}

// TestConsumerStartup_ReloadRacingInitialLoad reproduces a watcher event that
// arrives while the initial LoadSSRModules is still running (common right
// after boot: the editor saves a file while the app starts). Whatever the
// interleaving, the final style.css must equal the clean-startup output.
func TestConsumerStartup_ReloadRacingInitialLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns `go run`; skipped with -short")
	}

	root := writeFixtureApp(t)

	// Baseline: clean startup, no interleaved events.
	amClean := newConsumerStack(t, root)
	amClean.LoadSSRModules()
	amClean.WaitForSSRLoad(60 * time.Second)
	want := cssBody(t, amClean)

	// Race run: fire a watcher-style reload of one module concurrently with
	// the initial load.
	amRace := newConsumerStack(t, root)
	amRace.LoadSSRModules()
	_ = amRace.ReloadSSRModule(filepath.Join(root, "modules", "beta"))
	amRace.WaitForSSRLoad(60 * time.Second)
	got := cssBody(t, amRace)

	if got != want {
		t.Errorf("style.css differs when a reload event races the initial load — this is the run-to-run rendering difference seen in the consumer app\n clean startup:\n%s\n with racing event:\n%s", want, got)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
