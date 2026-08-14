package sitec

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/tinywasm/js"
)

func (c *AssetMin) UpdateFileContentInMemory(filePath, extension, event string, content []byte) (*asset, error) {
	file := &ContentFile{
		Path:    filePath,
		Content: content,
	}

	switch extension {
	case ".css":
		err := c.mainStyleCssHandler.UpdateContent(filePath, event, file)
		return c.mainStyleCssHandler, err

	case ".js":
		// Remove a leading "use strict" directive from incoming files to avoid
		// duplicating the directive which we add globally in startCodeJS.
		file.Content = js.StripLeadingUseStrict(file.Content)
		err := c.mainJsHandler.UpdateContent(filePath, event, file)
		return c.mainJsHandler, err

	case ".svg":
		// Check if it's the favicon file
		if filepath.Base(filePath) == c.faviconSvgHandler.fileOutputName {
			err := c.faviconSvgHandler.UpdateContent(filePath, event, file)
			return c.faviconSvgHandler, err
		}
		// Otherwise treat as sprite icon
		err := c.spriteSvgHandler.UpdateContent(filePath, event, file)
		return c.spriteSvgHandler, err

	case ".html":
		err := c.indexHtmlHandler.UpdateContent(filePath, event, file)
		return c.indexHtmlHandler, err
	}

	return nil, errors.New("UpdateFileContentInMemory extension: " + extension + " not found " + filePath)
}

func (c *AssetMin) isOutputPath(filePath string) bool {
	normalizedFilePath := filepath.Clean(filePath)
	cssOutputPath := filepath.Clean(c.mainStyleCssHandler.outputPath)
	jsOutputPath := filepath.Clean(c.mainJsHandler.outputPath)
	svgOutputPath := filepath.Clean(c.spriteSvgHandler.outputPath)
	faviconOutputPath := filepath.Clean(c.faviconSvgHandler.outputPath)
	htmlHandlerOutputPath := filepath.Clean(c.indexHtmlHandler.outputPath)

	if normalizedFilePath == cssOutputPath ||
		normalizedFilePath == jsOutputPath ||
		normalizedFilePath == svgOutputPath ||
		normalizedFilePath == faviconOutputPath ||
		normalizedFilePath == htmlHandlerOutputPath {
		return true
	}

	normalizedFilePathLower := strings.ToLower(normalizedFilePath)
	cssOutputPathLower := strings.ToLower(cssOutputPath)
	jsOutputPathLower := strings.ToLower(jsOutputPath)
	svgOutputPathLower := strings.ToLower(svgOutputPath)
	faviconOutputPathLower := strings.ToLower(faviconOutputPath)
	htmlHandlerOutputPathLower := strings.ToLower(htmlHandlerOutputPath)

	return normalizedFilePathLower == cssOutputPathLower ||
		normalizedFilePathLower == jsOutputPathLower ||
		normalizedFilePathLower == svgOutputPathLower ||
		normalizedFilePathLower == faviconOutputPathLower ||
		normalizedFilePathLower == htmlHandlerOutputPathLower
}

func (c *AssetMin) NewFileEvent(fileName, extension, filePath, event string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isOutputPath(filePath) {
		return nil
	}

	if extension == ".go" {
		fn := c.onSSRCompile
		if fn != nil {
			c.mu.Unlock()
			err := fn()
			c.mu.Lock()
			return err
		}
		return nil
	}

	ssr := c.isSSRMode()
	if ssr {
		switch extension {
		case ".css", ".js", ".svg", ".html":
			dir := filepath.Dir(filePath)
			c.mu.Unlock()
			_ = c.ReloadSSRModule(dir)
			c.mu.Lock()
			return nil
		}
		return nil
	}

	var content []byte
	var err error

	if event == "remove" || event == "delete" {
		content = []byte{}
	} else {
		c.mu.Unlock()
		content, err = os.ReadFile(filePath)
		c.mu.Lock()
		if err != nil {
			return errors.New("NewFileEvent read file error: " + err.Error())
		}
	}

	if extension == ".svg" && filepath.Base(filePath) != c.faviconSvgHandler.fileOutputName {
		if err := c.addIconFile(fileName, string(content)); err != nil {
			return err
		}
		return c.processAsset(fhForUnlocks(c, c.spriteSvgHandler))
	}
	fh, err := c.UpdateFileContentInMemory(filePath, extension, event, content)
	if err != nil {
		return err
	}
	if fh == nil {
		return nil
	}

	return c.processAsset(fh)
}

func fhForUnlocks(c *AssetMin, fh *asset) *asset {
	return fh
}

func (c *AssetMin) processAsset(fh *asset) error {
	// 1. Always regenerate cache
	if err := fh.RegenerateCache(c.activeMinifier()); err != nil {
		return err
	}

	// 2. Write to disk only if enabled
	if c.diskMirrored {
		return c.fs.Write(fh.outputPath, fh.GetCachedMinified(), fh.mediatype)
	}
	return nil
}

func (c *AssetMin) UnobservedFiles() []string {
	// Only truly generated/merged files should be unobserved.
	// index.html and favicon.svg are often user-editable.
	out := []string{
		c.mainStyleCssHandler.outputPath,
		c.mainJsHandler.outputPath,
		c.spriteSvgHandler.outputPath,
	}
	if c.imageProcessor != nil {
		out = append(out, c.imageProcessor.UnobservedFiles()...)
	}
	out = append(out, c.fontOutputPaths()...)
	return out
}

func (c *AssetMin) startCodeJS() (out string, err error) {
	c.wasmMu.Lock()
	runtime := c.wasmRuntime
	filename := c.wasmFilename
	c.wasmMu.Unlock()

	out = js.UseStrictPrefix
	if runtime != "" {
		if filename != "" && filename != "client.wasm" {
			runtime = strings.ReplaceAll(runtime, "/client.wasm", "/"+filename)
		}
		out += "\n" + runtime
	}
	return
}

// clear memory files
func (f *asset) ClearMemoryFiles() {
	f.contentOpen = []*ContentFile{}
	f.contentMiddle = []*ContentFile{}
	f.contentClose = []*ContentFile{}
}
