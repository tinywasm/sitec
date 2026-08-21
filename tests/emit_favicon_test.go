//go:build !wasm

package sitec_test

import (
	"github.com/tinywasm/router/mock"
	"github.com/tinywasm/sitec"
	"github.com/tinywasm/sitec/serve"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFaviconProcessing(t *testing.T) {
	t.Run("favicon_svg_processed_to_output", func(t *testing.T) {
		env := setupTestEnv("favicon_svg_processed", t)
		env.AssetsHandler.FlushToDisk()
		env.CreatePublicDir()

		faviconContent := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle cx="50" cy="50" r="40" fill="#007acc"/></svg>`
		faviconPath := filepath.Join(env.BaseDir, "favicon.svg")
		os.WriteFile(faviconPath, []byte(faviconContent), 0644)

		if err := env.AssetsHandler.NewFileEvent("favicon.svg", ".svg", faviconPath, "create"); err != nil {
			t.Fatalf("Error: %v", err)
		}

		outputFaviconPath := filepath.Join(env.PublicDir, "favicon.svg")
		if _, err := os.Stat(outputFaviconPath); os.IsNotExist(err) {
			t.Fatalf("Favicon NOT found at %s", outputFaviconPath)
		}
	})

	t.Run("favicon_separate_from_sprite_icons", func(t *testing.T) {
		env := setupTestEnv("favicon_separate", t)
		env.AssetsHandler.FlushToDisk()
		env.CreatePublicDir()

		faviconPath := filepath.Join(env.BaseDir, "favicon.svg")
		os.WriteFile(faviconPath, []byte(`<svg id="fav"><circle/></svg>`), 0644)

		iconPath := filepath.Join(env.BaseDir, "home.svg")
		os.WriteFile(iconPath, []byte(`<svg id="home-svg" viewBox="0 0 24 24"><path d="M10 20v-6h4v6h5v-8h3L12 3 2 12h3v8z"/></svg>`), 0644)

		env.AssetsHandler.NewFileEvent("favicon.svg", ".svg", faviconPath, "create")
		env.AssetsHandler.NewFileEvent("home.svg", ".svg", iconPath, "create")

		// Favicon check
		favOut, _ := os.ReadFile(filepath.Join(env.PublicDir, "favicon.svg"))
		if !strings.Contains(string(favOut), "fav") {
			t.Error("Favicon output missing its content")
		}

		// Sprite check
		if !env.AssetsHandler.ContainsSVG("home") {
			t.Error("Sprite bundle missing 'home' icon")
		}
	})
}

func TestFaviconCacheHeaders(t *testing.T) {
	setup := newTestSetup(t)
	setup.ac.DevMode = true
	am := sitec.NewAssetMin(setup.ac)

	faviconPath := filepath.Join(t.TempDir(), "favicon.svg")
	os.WriteFile(faviconPath, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><circle cx="50" cy="50" r="40"/></svg>`), 0644)
	if err := am.NewFileEvent("favicon.svg", ".svg", faviconPath, "create"); err != nil {
		t.Fatalf("NewFileEvent: %v", err)
	}

	r := &mock.Router{}
	serve.RegisterRoutes(r, am)

	ctxFavicon := &mock.Context{InPath: "/favicon.svg", InMethod: "GET"}
	r.Invoke("GET", "/favicon.svg", ctxFavicon)
	if !strings.Contains(string(ctxFavicon.ResponseBody()), "<svg") {
		t.Errorf("expected favicon SVG in response, got %q", string(ctxFavicon.ResponseBody()))
	}
}
