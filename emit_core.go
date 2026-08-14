package sitec

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
	minifySvg "github.com/tdewolff/minify/v2/svg"
	twcss "github.com/tinywasm/css"
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/font"
	"github.com/tinywasm/svg/sprite"
)

type AssetMin struct {
	mu sync.Mutex // Mutex for synchronization
	*Config
	mainStyleCssHandler *asset
	mainJsHandler       *asset
	spriteSvgHandler    *asset
	faviconSvgHandler   *asset
	indexHtmlHandler    *asset
	min                 *minify.M
	ssrEnabled          bool              // SSR branch activation flag
	InitialLoadFailed   bool              // true tras agotar los reintentos de ExtractAll; el próximo evento SSR debe reintentar el escaneo completo
	diskMirrored        bool              // If true, assets are being mirrored to disk
	allAssets           map[string]*asset // Keyed by outputPath - dedup
	log                 func(message ...any)
	onSSRCompile        func() error
	ssrLoading          sync.WaitGroup
	minifyEnabled       bool
	fromRoot            *rootCandidate
	fromCss             *rootCandidate
	standaloneJS        map[string]*asset
	standaloneOwners    map[string][]string // module name -> list of standalone asset names (outputs)
	imageProcessor      ImageProcessor
	ssrExtractor        SSRExtractor
	moduleSprites       map[string]*sprite.Sprite
	spriteMu            sync.RWMutex
	fontsMu             sync.RWMutex
	fonts               font.Declaration // root module only; zero-value = none
	fs                  FS
	wasmFilename        string
	wasmRuntime         string
	wasmMu              sync.Mutex
	directArtifacts     []Artifact // pre-built binaries written via Write(), e.g. the WASM binary
}

func (c *AssetMin) SetFS(fs FS) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fs = fs
}

func (c *AssetMin) SetWasm(filename string, runtime string) {
	c.wasmMu.Lock()
	defer  c.wasmMu.Unlock()
	c.wasmFilename = filename
	c.wasmRuntime = runtime
}

type ImageProcessor interface {
	UnobservedFiles() []string
}

type SSRExtractor interface {
	ExtractModule(moduleDir string) (*Assets, error)
	ExtractAll() ([]*Assets, error)
}

func (c *AssetMin) activeMinifier() *minify.M {
	if c.minifyEnabled {
		return c.min
	}
	return nil
}

func (c *AssetMin) SetSSRExtractor(e SSRExtractor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ssrExtractor = e
}

func (c *AssetMin) SetImageProcessor(ip ImageProcessor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.imageProcessor = ip
}

func (c *AssetMin) LoadSSRModules() {
	c.mu.Lock()
	c.ssrEnabled = true
	c.mu.Unlock()
	c.ssrLoading.Add(1)
	go func() {
		defer c.ssrLoading.Done()
		c.mu.Lock()
		extractor := c.ssrExtractor
		c.mu.Unlock()
		if extractor == nil {
			return
		}
		all, err := extractor.ExtractAll()
		if err != nil {
			c.writeMessage("SSR extract error:", err)
			return
		}
		c.mu.Lock()
		for _, a := range all {
			if a == nil {
				continue
			}
			if err := c.routeAssets(a, a.IsRoot, a.IsFramework); err != nil {
				c.writeMessage("route assets error:", err)
			}
		}
		c.resolveAndApplyRootCSS()
		c.mu.Unlock()
	}()
}

func (c *AssetMin) ReloadSSRModule(moduleDir string) error {
	c.mu.Lock()
	failed := c.InitialLoadFailed
	c.mu.Unlock()

	if failed {
		c.mu.Lock()
		c.InitialLoadFailed = false
		c.mu.Unlock()
		c.LoadSSRModules()
		return nil
	}

	c.mu.Lock()
	extractor := c.ssrExtractor
	c.mu.Unlock()
	if extractor == nil {
		return nil
	}
	a, err := extractor.ExtractModule(moduleDir)
	if err != nil {
		return err
	}
	if a == nil {
		return nil
	}
	c.mu.Lock()
	err = c.routeAssets(a, a.IsRoot, a.IsFramework)
	if err == nil {
		c.resolveAndApplyRootCSS()
	}
	c.mu.Unlock()
	return err
}

func (c *AssetMin) WaitForSSRLoad(timeout time.Duration) {
	ch := make(chan struct{})
	go func() {
		c.ssrLoading.Wait()
		close(ch)
	}()
	select {
	case <-ch:
	case <-time.After(timeout):
	}
}

type rootCandidate struct {
	name string
	css  string
}

type Config struct {
	OutputDir       string // eg: web/static, web/public, web/assets
	RootDir         string // Root directory of the project where go.mod exists
	AppName         string // Application name for templates (default: "MyApp")
	AssetsURLPrefix    string                 // New: for HTTP routes
	DevMode            bool                   // If true, disables caching (default: false)
}

func NewAssetMin(ac *Config) *AssetMin {
	c := &AssetMin{
		Config:            ac,
		min:               minify.New(),
		minifyEnabled:     true,
		standaloneJS:      make(map[string]*asset),
		standaloneOwners:  make(map[string][]string),
		moduleSprites:     make(map[string]*sprite.Sprite),
		fs:                NewOsFS(),
	}

	if c.AppName == "" {
		c.AppName = "MyApp"
	}

	c.allAssets = make(map[string]*asset)

	jsMainFileName := "script.js"
	cssMainFileName := "style.css"
	svgMainFileName := "icons.svg"
	svgFaviconFileName := "favicon.svg"
	htmlMainFileName := "index.html"

	c.mainStyleCssHandler = newAssetFile(cssMainFileName, "text/css", ac, nil)
	c.mainJsHandler = newAssetFile(jsMainFileName, "text/javascript", ac, nil)
	c.spriteSvgHandler = NewSvgHandler(ac, svgMainFileName)
	c.faviconSvgHandler = NewFaviconSvgHandler(ac, svgFaviconFileName)

	// Set URL paths before creating the index handler that depends on them
	c.mainStyleCssHandler.urlPath = path.Join("/", ac.AssetsURLPrefix, cssMainFileName)
	c.mainJsHandler.urlPath = path.Join("/", ac.AssetsURLPrefix, jsMainFileName)
	c.faviconSvgHandler.urlPath = path.Join("/", ac.AssetsURLPrefix, svgFaviconFileName)
	c.spriteSvgHandler.urlPath = path.Join("/", ac.AssetsURLPrefix, svgMainFileName)

	c.indexHtmlHandler = NewHtmlHandler(ac, htmlMainFileName, c.mainStyleCssHandler.GetURLPath(), c.mainJsHandler.GetURLPath(), c.faviconSvgHandler.GetURLPath())
	c.indexHtmlHandler.urlPath = "/" // Index is always at root
	c.min.Add("text/html", &html.Minifier{
		KeepDocumentTags: true,
		KeepEndTags:      true,
		KeepWhitespace:   true,
		KeepQuotes:       true,
	})

	c.min.AddFunc("text/css", css.Minify)
	c.min.AddFuncRegexp(regexp.MustCompile("^(application|text)/(x-)?(java|ecma)script$"), js.Minify)
	c.min.AddFunc("image/svg+xml", minifySvg.Minify)

	c.mainJsHandler.initCode = c.startCodeJS

	// Register main assets
	for _, a := range []*asset{
		c.mainStyleCssHandler, c.mainJsHandler,
		c.spriteSvgHandler, c.faviconSvgHandler, c.indexHtmlHandler,
	} {
		c.allAssets[a.outputPath] = a
	}

	// Automatic Sprite Injection:
	// Link the Sprite Handler to the HTML Handler so the sprite is injected dynamically
	// into the HTML body. This avoids manual injection in build scripts.
	c.indexHtmlHandler.AddDynamicContent(func() []byte {
		return []byte(c.renderSprite())
	})

	c.spriteSvgHandler.AddDynamicContent(func() []byte {
		return []byte(c.renderSprite())
	})

	// @font-face from the root declaration. Read inside the closure so a later
	// ReloadSSRModule updates the CSS without re-registering.
	prefix := path.Join("/", ac.AssetsURLPrefix)
	c.mainStyleCssHandler.AddDynamicContent(func() []byte {
		c.fontsMu.RLock()
		d := c.fonts
		c.fontsMu.RUnlock()
		if d.Family() == "" {
			return nil
		}
		return []byte(twcss.FontFaces(d, prefix).String())
	})

	return c
}

func (c *AssetMin) Name() string {
	return "ASSETS"
}

func (c *AssetMin) SetLog(f func(message ...any)) {
	c.log = f
}

func (c *AssetMin) Logger(messages ...any) {
	if c.log != nil {
		c.log(messages...)
	}
}

func (c *AssetMin) SupportedExtensions() []string {
	return []string{".js", ".css", ".svg", ".html"}
}

func (c *AssetMin) writeMessage(messages ...any) {
	c.Logger(messages...)
}

func (c *AssetMin) EnsureOutputDirectoryExists() {
	outputDir := c.OutputDir
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		c.writeMessage("dont create output dir", err)
	}
}

func (c *AssetMin) refreshAsset(extension string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var handlers []*asset
	switch extension {
	case ".js":
		handlers = append(handlers, c.mainJsHandler)
		for _, h := range c.standaloneJS {
			handlers = append(handlers, h)
		}
	case ".css":
		handlers = append(handlers, c.mainStyleCssHandler)
	case ".html":
		handlers = append(handlers, c.indexHtmlHandler)
	case ".svg":
		handlers = append(handlers, c.spriteSvgHandler)
	}

	for _, fh := range handlers {
		if err := c.processAsset(fh); err != nil {
			c.writeMessage("Error refreshing asset "+extension, err)
		}
	}
}

// RefreshJSAssets triggers a refresh of JS assets.
// Call this when the WASM binary changes to ensure they are up to date.
func (c *AssetMin) RefreshJSAssets() {
	c.refreshAsset(".js")
}

// readGoModulePath extracts the module path from go.mod (e.g., "example.com/demo")
func readGoModulePath(rootDir string) (string, error) {
	gomodPath := filepath.Join(rootDir, "go.mod")
	content, err := os.ReadFile(gomodPath)
	if err != nil {
		return "", err
	}

	lines := string(content)
	newlineIdx := findIndex(lines, "\n")
	if newlineIdx < 0 {
		newlineIdx = len(lines)
	}
	firstLine := lines[:newlineIdx]

	if len(firstLine) > 7 && firstLine[:7] == "module " {
		return firstLine[7:], nil
	}
	return "", fmt.Err("no module line in go.mod")
}

func findIndex(s string, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// FS implementation for AssetMin:
func (c *AssetMin) Read(path string) ([]byte, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, ok := c.allAssets[path]
	if !ok {
		return nil, "", false
	}
	content, err := a.GetMinifiedContent(c.activeMinifier())
	if err != nil {
		return nil, "", false
	}
	return content, a.mediatype, true
}

// Write writes a pre-built artifact (e.g. the compiled WASM binary) straight to
// the configured FS sink, bypassing the ContentFile-assembly and minification
// pipeline used for CSS/JS/HTML fragments.
//
// A compiled binary is a finished artifact, not a text fragment to concatenate:
// WriteContent joins fragments with "\n" between them, which corrupts a binary.
// And no minifier is registered for arbitrary binary mediatypes (only
// text/css, javascript, image/svg+xml, text/html are), so routing it through
// RegenerateCache made minifier.Bytes return ErrNotExist — an error that
// FlushToDisk's loop silently discarded, leaving the artifact's cache empty and
// the file written to disk at 0 bytes.
func (c *AssetMin) Write(outPath string, content []byte, mediatype string) error {
	c.mu.Lock()
	fs := c.fs
	outputDir := ""
	if c.Config != nil {
		outputDir = c.Config.OutputDir
	}
	c.directArtifacts = append(c.directArtifacts, Artifact{
		Path:      path.Join("/", outPath),
		Mediatype: mediatype,
	})
	c.mu.Unlock()

	if fs == nil {
		return fmt.Err("Write", outPath, ": no FS configured")
	}

	fullPath := outPath
	if outputDir != "" && !filepath.IsAbs(outPath) {
		fullPath = filepath.Join(outputDir, outPath)
	}
	return fs.Write(fullPath, content, mediatype)
}

func (c *AssetMin) List() []Artifact {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Artifact
	for _, a := range c.allAssets {
		content, err := a.GetMinifiedContent(c.activeMinifier())
		if err != nil {
			continue
		}
		out = append(out, Artifact{
			Path:      a.GetURLPath(),
			Mediatype: a.mediatype,
			Content:   content,
		})
	}
	out = append(out, c.directArtifacts...)
	return out
}

// assetmin v0.5.0: updated for svg v0.0.5
