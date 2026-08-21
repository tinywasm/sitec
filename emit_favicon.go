package sitec

import (
	"path"
	"strings"

	"github.com/tinywasm/image/favicon"
)

func (c *AssetMin) setFaviconFiles(files []favicon.File) {
	c.faviconMu.Lock()
	defer c.faviconMu.Unlock()
	c.faviconFiles = files
}

func (c *AssetMin) getFaviconFiles() []favicon.File {
	c.faviconMu.RLock()
	defer c.faviconMu.RUnlock()
	cp := make([]favicon.File, len(c.faviconFiles))
	copy(cp, c.faviconFiles)
	return cp
}

func (c *AssetMin) hasManualFaviconContent() bool {
	if c.faviconSvgHandler == nil {
		return false
	}
	c.faviconSvgHandler.mu.RLock()
	defer c.faviconSvgHandler.mu.RUnlock()
	for _, f := range c.faviconSvgHandler.contentOpen {
		if len(f.Content) > 0 {
			return true
		}
	}
	for _, f := range c.faviconSvgHandler.contentMiddle {
		if len(f.Content) > 0 {
			return true
		}
	}
	for _, f := range c.faviconSvgHandler.contentClose {
		if len(f.Content) > 0 {
			return true
		}
	}
	if len(c.faviconSvgHandler.cachedMinified) > 0 {
		return true
	}
	return false
}

func (c *AssetMin) getFirstFaviconURL() string {
	c.faviconMu.RLock()
	for _, f := range c.faviconFiles {
		if f.Rel != "" {
			prefix := ""
			if c.Config != nil {
				prefix = c.Config.AssetsURLPrefix
			}
			c.faviconMu.RUnlock()
			return path.Join("/", prefix, f.Name)
		}
	}
	c.faviconMu.RUnlock()
	if c.hasManualFaviconContent() {
		return c.faviconSvgHandler.GetURLPath()
	}
	return ""
}

func buildFaviconHeadLinks(files []favicon.File, prefix string) []byte {
	var sb strings.Builder
	first := true
	for _, f := range files {
		if f.Rel == "" {
			continue
		}
		if !first {
			sb.WriteString("\n\t")
		}
		first = false
		sb.WriteString(`<link rel="`)
		sb.WriteString(f.Rel)
		sb.WriteString(`"`)
		if f.Type != "" {
			sb.WriteString(` type="`)
			sb.WriteString(f.Type)
			sb.WriteString(`"`)
		}
		if f.Sizes != "" {
			sb.WriteString(` sizes="`)
			sb.WriteString(f.Sizes)
			sb.WriteString(`"`)
		}
		href := path.Join("/", prefix, f.Name)
		sb.WriteString(` href="`)
		sb.WriteString(href)
		sb.WriteString(`">`)
	}
	return []byte(sb.String())
}

func (c *AssetMin) updateHtmlFaviconLinks() {
	files := c.getFaviconFiles()
	prefix := ""
	if c.Config != nil {
		prefix = c.Config.AssetsURLPrefix
	}
	links := buildFaviconHeadLinks(files, prefix)
	if len(links) == 0 && c.hasManualFaviconContent() {
		href := c.faviconSvgHandler.GetURLPath()
		links = []byte(`<link rel="icon" type="image/svg+xml" href="` + href + `">`)
	}
	cssURL := ""
	jsURL := ""
	if c.mainStyleCssHandler != nil {
		cssURL = c.mainStyleCssHandler.GetURLPath()
	}
	if c.mainJsHandler != nil {
		jsURL = c.mainJsHandler.GetURLPath()
	}
	var linkSection string
	if len(links) > 0 {
		linkSection = string(links) + "\n\t"
	}
	openContent := []byte(`<!doctype html>
<html>
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
	<title></title>
	` + linkSection + `<link rel="stylesheet" href="` + cssURL + `" type="text/css" />
</head>
<body>`)
	if c.indexHtmlHandler == nil {
		return
	}
	c.indexHtmlHandler.mu.Lock()
	defer c.indexHtmlHandler.mu.Unlock()
	if len(c.indexHtmlHandler.contentOpen) > 0 {
		c.indexHtmlHandler.contentOpen[0].Content = openContent
		c.indexHtmlHandler.cacheValid = false
	} else {
		c.indexHtmlHandler.contentOpen = append(c.indexHtmlHandler.contentOpen, &ContentFile{
			Path:    "index-open.html",
			Content: openContent,
		})
		c.indexHtmlHandler.cacheValid = false
	}
	closeContent := []byte(`</div>
<script src="` + jsURL + `" type="text/javascript"></script>
</body>
</html>`)
	if len(c.indexHtmlHandler.contentClose) > 0 {
		c.indexHtmlHandler.contentClose[0].Content = closeContent
		c.indexHtmlHandler.cacheValid = false
	}
}
