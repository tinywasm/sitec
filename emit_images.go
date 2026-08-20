package sitec

import (
	"path"
)

// PublishImages mete las imágenes ya procesadas en el conjunto de artefactos
// en memoria (Artifacts()), que es lo único que el servidor de desarrollo
// consulta y lo que el release vuelca con WriteTo.
//
// Sin esto una imagen solo existía como archivo en el caché de conversión, así
// que la única manera de servirla era que el demonio creara un directorio de
// salida dentro del proyecto del usuario —una segunda salida del sitio, con
// bytes distintos a los del entregable de release.
//
// NO escribe en disco: escribir es responsabilidad de quien decide volcar
// (Site/Output.WriteTo, FlushToDisk), no un efecto secundario de publicar. El
// contenido sale idéntico porque el pipeline es determinista.
//
// Es idempotente: reescribe cada ruta con el contenido actual.
func (c *AssetMin) PublishImages() error {
	c.mu.Lock()
	ip := c.imageProcessor
	c.mu.Unlock()

	if ip == nil {
		return nil
	}

	for _, a := range ip.Artifacts() {
		urlKey := path.Join("/", a.Path)
		c.mu.Lock()
		c.directArtifacts = append(c.directArtifacts, Artifact{
			Path:      urlKey,
			Mediatype: a.Mediatype,
			Content:   a.Content,
		})
		c.mu.Unlock()
	}
	return nil
}
