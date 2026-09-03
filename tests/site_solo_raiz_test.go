//go:build !wasm

package sitec_test

import (
	"strings"
	"testing"

	"github.com/tinywasm/html"
	"github.com/tinywasm/sitec"
)

// TestSiteSoloElRaizDescribeElSitio: RenderSite() en un módulo que no es el
// raíz se avisa ruidosamente y se ignora — el sitio lo describe el proyecto,
// no una dependencia. Solo el error "dos raíces" falla el build.
func TestSiteSoloElRaizDescribeElSitio(t *testing.T) {
	var logs []string
	am := sitec.NewAssetMin(&sitec.Config{
		RootDir: "/tmp/fake-root",
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
		{
			ModuleName: "example.com/dep",
			IsRoot:     false,
			Site:       &sitec.Site{URL: "https://dep.example"},
		},
	})
	if err != nil {
		t.Fatalf("RenderSite() de un módulo no raíz no debe fallar el build: %v", err)
	}

	found := false
	for _, l := range logs {
		if strings.Contains(l, "example.com/dep") && strings.Contains(l, "is not the root project") {
			found = true
		}
	}
	if !found {
		t.Errorf("esperado aviso msgSiteNonRoot nombrando a example.com/dep, logs: %v", logs)
	}

	var sitemap string
	for _, a := range am.List() {
		if a.Path == "/sitemap.xml" {
			sitemap = string(a.Content)
		}
	}
	if !strings.Contains(sitemap, "https://acme.example") {
		t.Errorf("el sitemap debe usar la URL del raíz, no la del módulo no raíz; got:\n%s", sitemap)
	}
}

// TestSiteDosRaicesEsError: con la declaración pasa a ser posible detectar el
// caso imposible del extractor — dos módulos raíz declarando el sitio — y eso
// es un fallo de build, no un aviso: hay dos autoridades sobre el mismo sitio.
func TestSiteDosRaicesEsError(t *testing.T) {
	am := sitec.NewAssetMin(&sitec.Config{})
	am.SetLog(func(...any) {})

	err := am.RouteExtractedAssets([]*sitec.Assets{
		{
			ModuleName: "example.com/app",
			IsRoot:     true,
			Site:       &sitec.Site{URL: "https://acme.example"},
		},
		{
			ModuleName: "example.com/site-two",
			IsRoot:     true,
			Site:       &sitec.Site{URL: "https://other.example"},
		},
	})

	if err == nil {
		t.Fatal("esperado error al declarar RenderSite() en dos módulos raíz, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "example.com/app") || !strings.Contains(msg, "example.com/site-two") {
		t.Errorf("el error debe nombrar a ambos módulos, got: %v", msg)
	}
}
