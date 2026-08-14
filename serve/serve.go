package serve

import (
	"strings"

	"github.com/tinywasm/router"
	"github.com/tinywasm/sitec"
)

// RegisterRoutes registers the asset handlers on the router dynamically from the FS list.
func RegisterRoutes(r router.Router, fs sitec.FS) {
	for _, art := range fs.List() {
		a := art
		// Skip exposing the SVG sprite (icons.svg) as a separate route since it is injected in HTML
		if strings.HasSuffix(a.Path, "icons.svg") {
			continue
		}

		r.PublicAsset(a.Path, func(ctx router.Context) {
			ctx.SetHeader("Content-Type", a.Mediatype)

			// Robust check for HTML/JS regardless of charset
			isDevMutableText := strings.Contains(a.Mediatype, "text/")
			if isDevMutableText ||
				strings.Contains(a.Mediatype, "text/html") ||
				strings.Contains(a.Mediatype, "application/javascript") ||
				strings.Contains(a.Mediatype, "text/javascript") {
				ctx.SetHeader("Cache-Control", "no-cache, no-store, must-revalidate")
			} else {
				// Production or non-text assets (images, fonts, etc.): Strong cache
				ctx.SetHeader("Cache-Control", "public, max-age=31536000, immutable")
			}

			ctx.Write(a.Content)
		})
	}
}
