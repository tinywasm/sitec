package sitec

import (
	"path"
	"path/filepath"

	"github.com/tinywasm/fmt"
)

// PublishImages mete las imágenes ya procesadas en el FS, que es lo único que
// el servidor de desarrollo consulta.
//
// Sin esto una imagen solo existía como archivo en disco, así que la única
// manera de servirla era que el demonio creara un directorio de salida dentro
// del proyecto del usuario —una segunda salida del sitio, con bytes distintos
// a los del entregable de release.
//
// Es idempotente: reescribe cada ruta con el contenido actual.
func (c *AssetMin) PublishImages() error {
	c.mu.Lock()
	ip := c.imageProcessor
	fs := c.fs
	c.mu.Unlock()

	if ip == nil {
		return nil
	}
	if fs == nil {
		return fmt.Err("sitec: PublishImages sin FS configurado")
	}

	c.mu.Lock()
	outputDir := ""
	if c.Config != nil {
		outputDir = c.Config.OutputDir
	}
	c.mu.Unlock()

	for _, a := range ip.Artifacts() {
		// urlKey (sin OutputDir) es lo que resuelve Read/directArtifacts en cada
		// petición — así sirve desde memoria aunque el disco no exista o falle.
		// diskPath (con OutputDir) es la copia física, igual que outputPath para
		// el resto de assets: sin el prefijo, osFS.Write escribe relativo al cwd
		// del proceso, no del sitio — un directorio de salida fantasma.
		urlKey := path.Join("/", a.Path)
		diskPath := filepath.Join(outputDir, a.Path)

		c.mu.Lock()
		c.directArtifacts = append(c.directArtifacts, Artifact{
			Path:      urlKey,
			Mediatype: a.Mediatype,
			Content:   a.Content,
		})
		c.mu.Unlock()

		if err := fs.Write(diskPath, a.Content, a.Mediatype); err != nil {
			return err
		}
	}
	return nil
}
