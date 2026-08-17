//go:build !wasm

package sitec_test

import (
	"strings"
	"testing"

	"github.com/tinywasm/html"
	"github.com/tinywasm/sitec"
	"github.com/tinywasm/svg/sprite"
)

// TestPagesCarryEveryModulesIcons reproduces what shipped in mjosefa-website:
// the root project owns the pages, its dependencies own the glyphs, and the
// emitted index.html carried an EMPTY sprite — every <use href="#…"> in the
// markup pointed at a symbol that was not there, so no icon in the site drew.
//
// The order below is the one that matters: the page owner arrives FIRST, the
// module contributing icons after it. Rendering pages while still walking the
// module list bakes in whatever the sprite happens to hold at that moment.
func TestPagesCarryEveryModulesIcons(t *testing.T) {
	am := sitec.NewAssetMin(&sitec.Config{OutputDir: "web/public"})

	pageOwner := &sitec.Assets{
		ModuleName: "example.com/site",
		IsRoot:     true,
		Pages: []html.Page{
			{
				Path: "/",
				Doc:  html.DocumentOptions{Title: "Home"},
				Body: `<svg><use href="#nav-menu"/></svg>`,
			},
		},
	}

	iconOwner := &sitec.Assets{
		ModuleName: "example.com/components",
		Icons: sprite.NewSprite(
			sprite.Define("nav-menu", "0 0 24 24",
				sprite.Path("M3 18h18v-2H3v2zm0-5h18v-2H3v2zm0-7v2h18V6H3z"),
			),
		),
	}

	if err := am.RouteExtractedAssets([]*sitec.Assets{pageOwner, iconOwner}); err != nil {
		t.Fatalf("RouteExtractedAssets failed: %v", err)
	}

	pageBytes, _, ok := am.Read("web/public/index.html")
	if !ok {
		t.Fatal("expected index.html to be emitted")
	}
	page := string(pageBytes)

	if !strings.Contains(page, `id="nav-menu"`) {
		t.Errorf("index.html sprite is missing the dependency's symbol, so every <use href=\"#nav-menu\"> in the body resolves to nothing:\n%s", page)
	}
}
