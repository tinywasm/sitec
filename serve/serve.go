package serve

import (
	"path"
	"strings"

	"github.com/tinywasm/router"
	"github.com/tinywasm/sitec"
)

const (
	spriteFile    = "/icons.svg"
	notFoundBody  = "404 no encontrado"
	mediatypeText = "text/plain; charset=utf-8"
)

// RegisterRoutes expone el FS completo bajo una sola ruta comodín.
//
// Una ruta por artefacto no sirve: se registrarían en un instante y el sitio
// se completa después, así que el servidor quedaba sirviendo la foto del
// arranque —un style.css sin el CSS de ninguna dependencia— y las rutas
// nacidas más tarde no existían.
func RegisterRoutes(r router.Router, fs sitec.FS) {
	r.PublicAsset("/", func(ctx router.Context) {
		key := ctx.Path()

		if key == spriteFile || strings.HasSuffix(key, spriteFile) {
			ctx.SetHeader("Content-Type", mediatypeText)
			ctx.WriteStatus(404)
			ctx.Write([]byte(notFoundBody))
			return
		}

		content, mediatype, ok := fs.Read(key)
		if !ok {
			lastSegment := path.Base(key)
			if !strings.Contains(lastSegment, ".") {
				retryKey := strings.TrimRight(key, "/") + "/"
				content, mediatype, ok = fs.Read(retryKey)
			}
		}

		if !ok {
			ctx.SetHeader("Content-Type", mediatypeText)
			ctx.WriteStatus(404)
			ctx.Write([]byte(notFoundBody))
			return
		}

		ctx.SetHeader("Content-Type", mediatype)

		isDevMutableText := strings.Contains(mediatype, "text/")
		if isDevMutableText ||
			strings.Contains(mediatype, "text/html") ||
			strings.Contains(mediatype, "application/javascript") ||
			strings.Contains(mediatype, "text/javascript") {
			ctx.SetHeader("Cache-Control", "no-cache, no-store, must-revalidate")
		} else {
			ctx.SetHeader("Cache-Control", "public, max-age=31536000, immutable")
		}

		ctx.Write(content)
	})
}
