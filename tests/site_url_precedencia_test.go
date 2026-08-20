//go:build !wasm

package sitec_test

import (
	"strings"
	"testing"

	"github.com/tinywasm/html"
	"github.com/tinywasm/sitec"
)

// TestSiteURLMandaSobreConfig: cuando BuildConfig y RenderSite() traen URLs
// distintas, manda la del proyecto —y solo entonces— el aviso sale una vez,
// con los dos valores. El sitemap se construye con la URL efectiva.
func TestSiteURLMandaSobreConfig(t *testing.T) {
	var logs []string
	am := sitec.NewAssetMin(&sitec.Config{
		RootDir: "/tmp/fake-root",
		SiteURL: "https://caller.example",
	})
	am.SetLog(func(msgs ...any) {
		logs = append(logs, msgs[0].(string))
	})

	err := am.RouteExtractedAssets([]*sitec.Assets{
		{
			ModuleName: "example.com/app",
			IsRoot:     true,
			Site:       &sitec.Site{URL: "https://acme.example"},
			Pages: []html.Page{
				{
					Path: "index.html",
					Doc:  html.DocumentOptions{Title: "Acme"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RouteExtractedAssets failed: %v", err)
	}

	warned := 0
	for _, l := range logs {
		if strings.Contains(l, "https://acme.example") && strings.Contains(l, "https://caller.example") {
			warned++
		}
	}
	if warned != 1 {
		t.Errorf("el aviso de precedencia debe salir una sola vez con ambos valores, got %d\nlogs: %v", warned, logs)
	}

	var sitemap string
	for _, a := range am.List() {
		if a.Path == "/sitemap.xml" {
			sitemap = string(a.Content)
		}
	}
	if !strings.Contains(sitemap, "https://acme.example") {
		t.Errorf("el sitemap debe usar la URL del proyecto; got:\n%s", sitemap)
	}
}

// TestSiteURLSinConflictos: cuando coinciden, no hay aviso.
func TestSiteURLSinConflictos(t *testing.T) {
	var logs []string
	am := sitec.NewAssetMin(&sitec.Config{
		RootDir: "/tmp/fake-root",
		SiteURL: "https://acme.example",
	})
	am.SetLog(func(msgs ...any) {
		logs = append(logs, msgs[0].(string))
	})

	err := am.RouteExtractedAssets([]*sitec.Assets{
		{
			ModuleName: "example.com/app",
			IsRoot:     true,
			Site:       &sitec.Site{URL: "https://acme.example"},
			Pages: []html.Page{
				{
					Path: "index.html",
					Doc:  html.DocumentOptions{Title: "Acme"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RouteExtractedAssets failed: %v", err)
	}

	for _, l := range logs {
		if strings.Contains(l, "manda la del proyecto") {
			t.Errorf("sin conflicto no debe haber aviso de precedencia, got: %v", l)
		}
	}
}
