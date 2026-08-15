//go:build !wasm

package sitec_test

import (
	"strings"
	"testing"

	"github.com/tinywasm/html"
	"github.com/tinywasm/sitec"
)

type multiPageProvider struct{}

func (m *multiPageProvider) RenderPages() []html.Page {
	return []html.Page{
		{
			Path: "/",
			Doc: html.DocumentOptions{
				Title:       "Home Page Title",
				Description: "Home Page Description",
			},
			Body: "<div id='home'>Home Content</div>",
		},
		{
			Path: "/especialidades/oftalmologia/",
			Doc: html.DocumentOptions{
				Title:       "Oftalmología Chillán",
				Description: "Consulta de oftalmología en Chillán",
				Canonical:   "/especialidades/oftalmologia/",
			},
			Body: "<div id='oftalmo'>Oftalmología Content</div>",
		},
	}
}

type singleHTMLProvider struct{}

func (s *singleHTMLProvider) RenderHTML() string {
	return "<div>Single Page Content</div>"
}

func TestEmitPages_MultiPageEmission(t *testing.T) {
	ac := &sitec.Config{
		OutputDir: "web/public",
		SiteURL:   "https://clinic.example.com",
	}
	am := sitec.NewAssetMin(ac)

	provider := &multiPageProvider{}
	assets := &sitec.Assets{
		ModuleName: "example.com/clinic",
		Pages:      provider.RenderPages(),
	}

	err := am.RouteExtractedAssets([]*sitec.Assets{assets})
	if err != nil {
		t.Fatalf("RouteExtractedAssets failed: %v", err)
	}

	// 1. Verify index.html
	indexPath := "web/public/index.html"
	indexBytes, _, ok := am.Read(indexPath)
	if !ok {
		t.Fatalf("expected %s to exist", indexPath)
	}
	indexStr := string(indexBytes)

	if !strings.Contains(indexStr, "<title>Home Page Title</title>") {
		t.Errorf("index.html missing correct title, got: %s", indexStr)
	}
	if !strings.Contains(indexStr, "content='Home Page Description'") && !strings.Contains(indexStr, `content="Home Page Description"`) {
		t.Errorf("index.html missing home description, got: %s", indexStr)
	}
	if !strings.Contains(indexStr, "<div id='home'>Home Content</div>") && !strings.Contains(indexStr, `<div id="home">Home Content</div>`) {
		t.Errorf("index.html missing home body content, got: %s", indexStr)
	}

	// 2. Verify especialidades/oftalmologia/index.html
	subPath := "web/public/especialidades/oftalmologia/index.html"
	subBytes, _, ok := am.Read(subPath)
	if !ok {
		t.Fatalf("expected %s to exist", subPath)
	}
	subStr := string(subBytes)

	if !strings.Contains(subStr, "Oftalmología Chillán") {
		t.Errorf("subpage missing correct title, got: %s", subStr)
	}
	if !strings.Contains(subStr, "Consulta de oftalmología en Chillán") {
		t.Errorf("subpage missing description, got: %s", subStr)
	}
	if !strings.Contains(subStr, "https://clinic.example.com/especialidades/oftalmologia/") {
		t.Errorf("subpage canonical URL not resolved to absolute, got: %s", subStr)
	}
	if !strings.Contains(subStr, "Oftalmología Content") {
		t.Errorf("subpage body missing content, got: %s", subStr)
	}
}

func TestEmitPages_Collision_RenderHTML_and_RenderPages(t *testing.T) {
	ac := &sitec.Config{
		OutputDir: "web/public",
	}
	am := sitec.NewAssetMin(ac)

	htmlAsset := &sitec.Assets{
		ModuleName: "example.com/modA",
		HTML:       "<div>HTML Component</div>",
	}
	pageAsset := &sitec.Assets{
		ModuleName: "example.com/modB",
		Pages: []html.Page{
			{
				Path: "/",
				Doc:  html.DocumentOptions{Title: "Home"},
				Body: "<div>Home Page</div>",
			},
		},
	}

	err := am.RouteExtractedAssets([]*sitec.Assets{htmlAsset, pageAsset})
	if err == nil {
		t.Fatalf("expected collision error when RenderHTML and RenderPages coincide on /, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "example.com/modA") || !strings.Contains(errStr, "example.com/modB") {
		t.Errorf("expected collision error to name both conflicting modules, got: %v", err)
	}
}

func TestEmitPages_Collision_DuplicatePages(t *testing.T) {
	ac := &sitec.Config{
		OutputDir: "web/public",
	}
	am := sitec.NewAssetMin(ac)

	pageAsset1 := &sitec.Assets{
		ModuleName: "example.com/mod1",
		Pages: []html.Page{
			{
				Path: "/about/",
				Doc:  html.DocumentOptions{Title: "About Mod 1"},
			},
		},
	}
	pageAsset2 := &sitec.Assets{
		ModuleName: "example.com/mod2",
		Pages: []html.Page{
			{
				Path: "/about/",
				Doc:  html.DocumentOptions{Title: "About Mod 2"},
			},
		},
	}

	err := am.RouteExtractedAssets([]*sitec.Assets{pageAsset1, pageAsset2})
	if err == nil {
		t.Fatalf("expected collision error for duplicate page paths, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "example.com/mod1") || !strings.Contains(errStr, "example.com/mod2") {
		t.Errorf("expected collision error to name both modules, got: %v", err)
	}
}

func TestEmitPages_RenderHTML_Only_Regression(t *testing.T) {
	ac := &sitec.Config{
		OutputDir: "web/public",
	}
	am := sitec.NewAssetMin(ac)

	htmlAsset := &sitec.Assets{
		ModuleName: "example.com/legacy",
		HTML:       "<div>Legacy Single Page</div>",
	}

	err := am.RouteExtractedAssets([]*sitec.Assets{htmlAsset})
	if err != nil {
		t.Fatalf("unexpected error for RenderHTML-only module: %v", err)
	}

	indexPath := "web/public/index.html"
	indexBytes, _, ok := am.Read(indexPath)
	if !ok {
		t.Fatalf("expected %s to exist", indexPath)
	}

	if !strings.Contains(string(indexBytes), "Legacy Single Page") {
		t.Errorf("index.html missing legacy content, got: %s", string(indexBytes))
	}
}
