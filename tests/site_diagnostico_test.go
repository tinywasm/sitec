//go:build !wasm

package sitec_test

import (
	"strings"
	"testing"

	"github.com/tinywasm/html"
	"github.com/tinywasm/sitec"
)

// TestSiteSinPagesEsError: con RenderSite() la intención es explícita —un
// sitio estático— y su dueño de index.html es RenderPages(). Sin páginas, la
// salida sería el shell de una aplicación: publicarla sin un solo error es
// exactamente la clase de fallo silencioso que este check elimina.
func TestSiteSinPagesEsError(t *testing.T) {
	am := sitec.NewAssetMin(&sitec.Config{})
	am.SetLog(func(...any) {})

	err := am.RouteExtractedAssets([]*sitec.Assets{
		{
			ModuleName: "example.com/app",
			IsRoot:     true,
			Site:       &sitec.Site{URL: "https://acme.example"},
		},
	})

	if err == nil {
		t.Fatal("RenderSite() sin RenderPages() debe ser error de build, got nil")
	}
	if !strings.Contains(err.Error(), "RenderSite") || !strings.Contains(err.Error(), "RenderPages") {
		t.Errorf("el error debe nombrar ambas declaraciones, got: %v", err)
	}
}

// TestSiteAusenteEnProyectoConPages: RenderPages() sin RenderSite() —páginas
// que saldrán sin sitemap ni activos estáticos— es aviso, no error: la app
// con páginas SSR sigue siendo un caso válido, solo está incompleta.
func TestSiteAusenteEnProyectoConPages(t *testing.T) {
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
			Pages: []html.Page{
				{
					Path: "index.html",
					Doc:  html.DocumentOptions{Title: "Acme"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderPages() sin RenderSite() no debe fallar el build: %v", err)
	}

	for _, l := range logs {
		if strings.Contains(l, "RenderPages") && strings.Contains(l, "RenderSite") {
			return
		}
	}
	t.Errorf("esperado aviso msgPagesWithoutSite, logs: %v", logs)
}
