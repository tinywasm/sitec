//go:build !wasm

package sitec_test

import (
	"strings"
	"testing"

	"github.com/tinywasm/html"
	"github.com/tinywasm/sitec"
)

func TestEmitSitemap_WithSiteURL(t *testing.T) {
	ac := &sitec.Config{
		OutputDir: "web/public",
		SiteURL:   "https://clinic.example.com",
	}
	am := sitec.NewAssetMin(ac)

	assets := &sitec.Assets{
		ModuleName: "example.com/clinic",
		Pages: []html.Page{
			{
				Path: "/",
				Doc:  html.DocumentOptions{Title: "Home"},
			},
			{
				Path: "/especialidades/oftalmologia/",
				Doc:  html.DocumentOptions{Title: "Oftalmología"},
			},
		},
	}

	err := am.RouteExtractedAssets([]*sitec.Assets{assets})
	if err != nil {
		t.Fatalf("RouteExtractedAssets failed: %v", err)
	}

	sitemapBytes, _, ok := am.Read("web/public/sitemap.xml")
	if !ok {
		t.Fatalf("expected sitemap.xml to be emitted when SiteURL is set")
	}

	sitemapStr := string(sitemapBytes)

	if !strings.Contains(sitemapStr, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>") {
		t.Errorf("sitemap.xml missing XML header, got: %s", sitemapStr)
	}
	if !strings.Contains(sitemapStr, "<loc>https://clinic.example.com/</loc>") {
		t.Errorf("sitemap.xml missing root URL, got: %s", sitemapStr)
	}
	if !strings.Contains(sitemapStr, "<loc>https://clinic.example.com/especialidades/oftalmologia/</loc>") {
		t.Errorf("sitemap.xml missing subpage URL, got: %s", sitemapStr)
	}
}

func TestEmitSitemap_WithoutSiteURL(t *testing.T) {
	ac := &sitec.Config{
		OutputDir: "web/public",
		SiteURL:   "", // Empty SiteURL
	}
	am := sitec.NewAssetMin(ac)

	assets := &sitec.Assets{
		ModuleName: "example.com/clinic",
		Pages: []html.Page{
			{
				Path: "/",
				Doc:  html.DocumentOptions{Title: "Home"},
			},
		},
	}

	err := am.RouteExtractedAssets([]*sitec.Assets{assets})
	if err != nil {
		t.Fatalf("RouteExtractedAssets failed: %v", err)
	}

	_, _, ok := am.Read("web/public/sitemap.xml")
	if ok {
		t.Fatalf("sitemap.xml should NOT be emitted when SiteURL is empty")
	}
}
