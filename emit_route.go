package sitec

import (
	"sort"
	"strings"

	"github.com/tinywasm/dom"
	"github.com/tinywasm/fmt"
	twhtml "github.com/tinywasm/html"
)

type pageBodyComponent struct {
	dom.Element
	content string
}

func (p *pageBodyComponent) String() string {
	return p.content
}

// routeAssets routes one module's assets. Es la MITAD de compilacion.
// Decide que modulo aporta el RootCSS, los slots de cascada y la copia de
// fuentes: politica de como se ensambla la salida, no de cuando se reintenta.
func (c *AssetMin) routeAssets(a *Assets, isRoot, isFramework bool) error {
	if isRoot {
		c.fromRoot = nil
	} else if isFramework {
		c.fromCss = nil
	}

	// RenderSite(): solo el raíz describe el sitio. El raíz manda sobre
	// BuildConfig (URL efectiva resuelta una sola vez, aquí, en el campo ya
	// existente c.SiteURL — los puntos de uso, sitemap y canónicas, no repiten
	// la decisión). Un módulo no raíz que la declare: aviso ruidoso e ignorada.
	if a.Site != nil {
		if isRoot {
			c.site = a.Site
			if a.Site.URL != "" {
				if callerURL := c.SiteURL; callerURL != "" && callerURL != a.Site.URL {
					c.Logger(fmt.Sprintf(msgSiteURLPrecedence, a.Site.URL, callerURL))
				}
				c.SiteURL = a.Site.URL
			}
		} else {
			c.Logger(fmt.Sprintf(msgSiteNonRoot, a.ModuleName))
		}
	} else if isRoot {
		// El raíz dejó de declarar RenderSite() (reload): deja de ser un sitio.
		c.site = nil
	}

	if a.RootCSS != "" {
		switch {
		case isRoot:
			c.fromRoot = &rootCandidate{name: a.ModuleName, css: a.RootCSS}
		case isFramework:
			c.fromCss = &rootCandidate{name: a.ModuleName, css: a.RootCSS}
		default:
			return fmt.Err("module", a.ModuleName, "declares RootCSS() but is neither the root project nor", cssModulePath, "— rejected instead of silently ignored, since serving it would silently swap in the wrong theme")
		}
	}

	if a.Fonts.Family() != "" {
		if isRoot {
			if err := c.copyDeclaredFonts(a.Fonts); err != nil {
				return err
			}
			c.setFonts(a.Fonts)
		} else {
			c.Logger("warning: module", a.ModuleName, "declares Fonts() but only the root project may; ignoring")
		}
	} else if isRoot {
		c.setFonts(a.Fonts) // clear if root no longer declares fonts
	}

	slot := "middle"
	if isRoot {
		// El slot "close" para el raíz es una decisión de cascada CSS/JS
		// (su CSS gana sobre el de los módulos). El HTML IGNORA el slot:
		// updateSSRModuleInSlot lo rutea siempre a "middle", porque
		// contentClose arranca con `</div>` — HTML en "close" quedaría fuera
		// de #app (para el raíz, incluso después de </html>).
		slot = "close"
	}
	// RootCSS deliberately NOT passed here — it has its own slot resolution above.
	c.updateSSRModuleInSlot(a.ModuleName, a.CSS, a.JS, a.HTML, a.Icons, slot)

	return nil
}

// emitPages renders a module's pages. It is separate from routeAssets because
// a page bakes the icon sprite into its body, and the sprite is only complete
// once EVERY module has registered its glyphs. Emitting pages inside
// routeAssets meant the page owner — the root project, which sorts first —
// was rendered before a single dependency had contributed, so the sprite went
// out empty and every <use href="#…"> in the markup resolved to nothing.
func (c *AssetMin) emitPages(a *Assets) error {
	for _, p := range a.Pages {
		outPath, urlPath := normalizePagePath(p.Path)
		doc := p.Doc
		if doc.CSSURL == "" {
			doc.CSSURL = c.mainStyleCssHandler.GetURLPath()
		}
		if doc.JSURL == "" {
			doc.JSURL = c.mainJsHandler.GetURLPath()
		}
		if doc.FaviconURL == "" {
			doc.FaviconURL = c.faviconSvgHandler.GetURLPath()
		}
		if c.SiteURL != "" && doc.Canonical != "" && !isAbsoluteURL(doc.Canonical) {
			doc.Canonical = resolveAbsoluteURL(c.SiteURL, doc.Canonical)
		}

		bodyContent := c.renderSpriteNoLock() + p.Body
		rendered := twhtml.DocumentString(doc, &pageBodyComponent{content: bodyContent})

		pageAsset := newAssetFile(outPath, "text/html", c.Config, nil)
		pageAsset.urlPath = urlPath
		pageAsset.UpdateContentInSlot("page", "write", &ContentFile{Path: "page", Content: []byte(rendered)}, "middle")
		c.allAssets[pageAsset.outputPath] = pageAsset

		if outPath == "index.html" {
			c.indexHtmlHandler = pageAsset
		}
	}

	return nil
}

func normalizePagePath(pPath string) (outPath string, urlPath string) {
	if pPath == "" || pPath == "/" || pPath == "index.html" {
		return "index.html", "/"
	}
	trimmed := strings.TrimPrefix(pPath, "/")
	if strings.HasSuffix(pPath, "/") {
		return trimmed + "index.html", pPath
	}
	return trimmed, pPath
}

func isAbsoluteURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

func resolveAbsoluteURL(baseURL, relURL string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(relURL, "/")
}

func (c *AssetMin) emitSitemapNoLock() {
	var urls []string
	for _, a := range c.allAssets {
		if a.mediatype == "text/html" {
			urls = append(urls, a.GetURLPath())
		}
	}
	if len(urls) == 0 {
		urls = []string{"/"}
	} else {
		urls = sortAndDedup(urls)
	}

	sitemapXML := buildSitemapXML(c.SiteURL, urls)
	sitemapAsset := newAssetFile("sitemap.xml", "application/xml", c.Config, nil)
	sitemapAsset.urlPath = "/sitemap.xml"
	sitemapAsset.UpdateContentInSlot("sitemap", "write", &ContentFile{Path: "sitemap", Content: []byte(sitemapXML)}, "middle")
	c.allAssets[sitemapAsset.outputPath] = sitemapAsset
}

func buildSitemapXML(siteURL string, urls []string) string {
	var sb strings.Builder
	sb.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	sb.WriteString("<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, u := range urls {
		abs := resolveAbsoluteURL(siteURL, u)
		sb.WriteString("  <url>\n    <loc>")
		sb.WriteString(abs)
		sb.WriteString("</loc>\n  </url>\n")
	}
	sb.WriteString("</urlset>")
	return sb.String()
}

func sortAndDedup(slice []string) []string {
	if len(slice) == 0 {
		return slice
	}
	sort.Strings(slice)
	out := make([]string, 0, len(slice))
	out = append(out, slice[0])
	for i := 1; i < len(slice); i++ {
		if slice[i] != slice[i-1] {
			out = append(out, slice[i])
		}
	}
	return out
}

func (c *AssetMin) resolveAndApplyRootCSS() {
	var entries []*ContentFile
	if c.fromRoot != nil {
		entries = append(entries, &ContentFile{Path: c.fromRoot.name, Content: []byte(c.fromRoot.css)})
	} else if c.fromCss != nil {
		entries = append(entries, &ContentFile{Path: c.fromCss.name, Content: []byte(c.fromCss.css)})
	}

	c.mainStyleCssHandler.mu.Lock()
	c.mainStyleCssHandler.contentOpen = entries
	c.mainStyleCssHandler.cacheValid = false
	c.mainStyleCssHandler.mu.Unlock()
}
