package sitec

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
	minifySvg "github.com/tdewolff/minify/v2/svg"
	"github.com/tdewolff/minify/v2/xml"
	twcss "github.com/tinywasm/css"
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/font"
	"github.com/tinywasm/image/favicon"
	imgmin "github.com/tinywasm/image/min"
	"github.com/tinywasm/svg/sprite"
)

const (
	msgSiteNonRoot        = "sitec: el módulo %s declara RenderSite() pero no es el proyecto raíz — solo el raíz describe el sitio; se ignora"
	msgTwoRootSites       = "sitec: RenderSite() declarada por dos módulos raíz: %s y %s"
	msgSiteURLPrecedence  = "sitec: aviso: RenderSite() declara URL %s y BuildConfig trae %s — manda la del proyecto"
	msgSiteWithoutPages   = "sitec: el proyecto declara RenderSite() (es un sitio estático) pero ningún módulo declara RenderPages(): la salida sería el shell de una aplicación, no un sitio"
	msgPagesWithoutSite   = "sitec: aviso: el proyecto declara RenderPages() pero no RenderSite(): la salida no tendrá sitemap ni activos estáticos"
	msgStaticAbsentPrefix = "sitec: activo estático declarado y ausente: "
	msgFaviconNonRoot     = "sitec: solo el modulo raiz puede declarar Favicon(); lo declara %s"
	msgNoFavicon          = "sitec: el proyecto no declara Favicon(); las paginas saldran sin icono"
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
	site                *Site            // declarado por el raíz via RenderSite(); nil = el proyecto es una aplicación
	faviconFiles        []favicon.File
	faviconMu           sync.RWMutex
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

// SetMinifyEnabled toggles minification on or off. The consumer (app's TUI
// minify toggle) owns the UI; this is the only way to reach the flag, which
// was private before — activeMinifier() is the sole reader.
func (c *AssetMin) SetMinifyEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.minifyEnabled = enabled
}

// MinifyEnabled reports whether minification is currently on.
func (c *AssetMin) MinifyEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.minifyEnabled
}

func (c *AssetMin) SetWasm(filename string, runtime string) {
	c.wasmMu.Lock()
	defer c.wasmMu.Unlock()
	c.wasmFilename = filename
	c.wasmRuntime = runtime
}

type ImageProcessor interface {
	UnobservedFiles() []string
	Artifacts() []imgmin.Artifact
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
		if err := c.RouteExtractedAssets(all); err != nil {
			c.writeMessage("route assets error:", err)
		}
	}()
}

// RouteExtractedAssets applies a batch of already-extracted module assets —
// deciding which module's RootCSS wins, appending every module's RenderCSS to
// its slot, and copying declared fonts — then resolves the final root
// stylesheet. It is the routing half of LoadSSRModules, exported so a caller
// that needs its own retry/backoff policy around ExtractAll (app's
// AssetsHandler does) can still reach routing: routeAssets and
// resolveAndApplyRootCSS are unexported, and Go does not promote them through
// embedding across package boundaries.
func (c *AssetMin) RouteExtractedAssets(all []*Assets) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Las rutas del CSS/JS/favicon/sprite principales son relativas cuando el
	// build no declara ninguna página: un único index.html en la raíz (el
	// shell de una app WASM, sin RenderPages) puede montarse bajo cualquier
	// prefijo — dominio raíz, subpath de un servidor de preview, etc. — y una
	// ruta relativa lo resuelve sin importar dónde. Absolutas desde "/" sólo
	// son correctas cuando SÍ hay páginas, porque entonces pueden vivir a
	// distinta profundidad (ver TestEmitPages_MultiPageEmission, con una
	// página en "/especialidades/oftalmologia/") y sólo una referencia
	// absoluta desde el dominio las alcanza a todas por igual. Recalculado en
	// cada llamada (no sólo la primera) para que un hot-reload que agrega o
	// quita páginas no deje una ruta obsoleta.
	hasPages := false
	for _, a := range all {
		if a != nil && len(a.Pages) > 0 {
			hasPages = true
			break
		}
	}

	cssFile := filepath.Base(c.mainStyleCssHandler.urlPath)
	jsFile := filepath.Base(c.mainJsHandler.urlPath)
	faviconFile := filepath.Base(c.faviconSvgHandler.urlPath)
	spriteFile := filepath.Base(c.spriteSvgHandler.urlPath)

	if hasPages {
		c.mainStyleCssHandler.urlPath = path.Join("/", c.Config.AssetsURLPrefix, cssFile)
		c.mainJsHandler.urlPath = path.Join("/", c.Config.AssetsURLPrefix, jsFile)
		c.faviconSvgHandler.urlPath = path.Join("/", c.Config.AssetsURLPrefix, faviconFile)
		c.spriteSvgHandler.urlPath = path.Join("/", c.Config.AssetsURLPrefix, spriteFile)
	} else {
		c.mainStyleCssHandler.urlPath = path.Join(c.Config.AssetsURLPrefix, cssFile)
		c.mainJsHandler.urlPath = path.Join(c.Config.AssetsURLPrefix, jsFile)
		c.faviconSvgHandler.urlPath = path.Join(c.Config.AssetsURLPrefix, faviconFile)
		c.spriteSvgHandler.urlPath = path.Join(c.Config.AssetsURLPrefix, spriteFile)
	}

	// 0. RenderSite(): solo el raíz describe el sitio, y solo uno. El aviso de
	// módulo no raíz lo emite routeAssets (cubre también el camino de reload).
	// Aquí solo se detecta el caso que un single-module reload no puede ver:
	// dos raíces declarándola.
	var siteOwner string
	for _, a := range all {
		if a == nil || a.Site == nil || !a.IsRoot {
			continue
		}
		if siteOwner != "" {
			return fmt.Err(fmt.Sprintf(msgTwoRootSites, siteOwner, a.ModuleName))
		}
		siteOwner = a.ModuleName
	}

	// 0.5 Favicon(): solo el raíz puede declarar. Derivar y escribir el juego completo.
	var faviconOwner string
	var faviconSrc *favicon.Source
	for _, a := range all {
		if a == nil || a.Favicon == nil {
			continue
		}
		if !a.IsRoot {
			return fmt.Err(fmt.Sprintf(msgFaviconNonRoot, a.ModuleName))
		}
		if faviconOwner != "" {
			return fmt.Err(fmt.Sprintf(msgFaviconNonRoot, a.ModuleName))
		}
		faviconOwner = a.ModuleName
		faviconSrc = &favicon.Source{Raster: a.Favicon.Raster, SVG: a.Favicon.SVG}
	}
	if faviconOwner != "" {
		files, err := favicon.Derive(*faviconSrc)
		if err != nil {
			return err
		}
		c.faviconMu.Lock()
		c.faviconFiles = files
		c.faviconMu.Unlock()
		for _, f := range files {
			urlKey := path.Join("/", f.Name)
			c.directArtifacts = append(c.directArtifacts, Artifact{Path: urlKey, Mediatype: f.Mediatype, Content: f.Content})
			if c.fs != nil {
				outputDir := ""
				if c.Config != nil {
					outputDir = c.Config.OutputDir
				}
				fullPath := f.Name
				if outputDir != "" && !filepath.IsAbs(f.Name) {
					fullPath = filepath.Join(outputDir, f.Name)
				}
				if err := c.fs.Write(fullPath, f.Content, f.Mediatype); err != nil {
					return err
				}
			}
		}
		delete(c.allAssets, c.faviconSvgHandler.outputPath)
		c.updateHtmlFaviconLinks()
	} else {
		var faviconPath string
		if c.Config != nil && c.Config.OutputDir != "" {
			if filepath.IsAbs(c.Config.OutputDir) {
				faviconPath = filepath.Join(c.Config.OutputDir, "favicon.svg")
			} else if c.Config.RootDir != "" {
				faviconPath = filepath.Join(c.Config.RootDir, c.Config.OutputDir, "favicon.svg")
			} else {
				faviconPath = filepath.Join(c.Config.OutputDir, "favicon.svg")
			}
		} else {
			faviconPath = "favicon.svg"
		}
		if info, err := os.Stat(faviconPath); err == nil && info.Size() > 0 {
			c.faviconMu.Lock()
			c.faviconFiles = []favicon.File{{Name: "favicon.svg", Mediatype: "image/svg+xml", Rel: "icon", Type: "image/svg+xml"}}
			c.faviconMu.Unlock()
			delete(c.allAssets, c.faviconSvgHandler.outputPath)
			c.updateHtmlFaviconLinks()
		} else {
			c.faviconMu.Lock()
			c.faviconFiles = nil
			c.faviconMu.Unlock()
			delete(c.allAssets, c.faviconSvgHandler.outputPath)
			c.updateHtmlFaviconLinks()
			c.Logger(msgNoFavicon)
		}
	}

	// 1. Check page collisions across modules
	var htmlModule string
	pageOwners := make(map[string]string)

	for _, a := range all {
		if a == nil {
			continue
		}
		if a.HTML != "" {
			htmlModule = a.ModuleName
		}
		for _, p := range a.Pages {
			outPath, _ := normalizePagePath(p.Path)
			if existingOwner, exists := pageOwners[outPath]; exists {
				if existingOwner == a.ModuleName {
					return fmt.Err("ssr: page collision in module", a.ModuleName, ": multiple pages with path", p.Path)
				}
				return fmt.Err("ssr: page collision at", p.Path, ": declared by module", existingOwner, "and module", a.ModuleName)
			}
			pageOwners[outPath] = a.ModuleName
		}
	}

	if htmlModule != "" {
		if indexOwner, exists := pageOwners["index.html"]; exists {
			if htmlModule == indexOwner {
				return fmt.Err("ssr: page collision at /: module", htmlModule, "declares both RenderHTML and RenderPages with Path \"/\"")
			}
			return fmt.Err("ssr: page collision at /: RenderHTML in module", htmlModule, "conflicts with RenderPages in module", indexOwner)
		}
	}

	// 2. Route standard assets — every module, before any page is rendered.
	for _, a := range all {
		if a == nil {
			continue
		}
		if err := c.routeAssets(a, a.IsRoot, a.IsFramework); err != nil {
			return err
		}
	}
	c.resolveAndApplyRootCSS()

	// 3. Render pages, now that the icon sprite holds every module's glyphs.
	for _, a := range all {
		if a == nil {
			continue
		}
		if err := c.emitPages(a); err != nil {
			return err
		}
	}

	// 3.5 Diagnóstico del dueño de index.html. Con RenderSite() la intención
	// ya es explícita, así que se puede exigir: sin páginas, la salida sería
	// el shell de una aplicación —publicar eso sin un solo error es lo que
	// este check elimina. A la inversa, páginas sin RenderSite() significan
	// un sitio que saldrá sin sitemap ni activos estáticos: aviso, no error.
	if c.site != nil && len(pageOwners) == 0 {
		return fmt.Err(msgSiteWithoutPages)
	}
	if c.site == nil && len(pageOwners) > 0 {
		c.Logger(msgPagesWithoutSite)
	}

	// 4. Emit sitemap.xml if SiteURL is set
	if c.SiteURL != "" {
		c.emitSitemapNoLock()
	}

	return nil
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
		// Same order as the full pass: assets first, then pages. Here the
		// other modules' glyphs are already registered from the initial load,
		// so re-rendering this module's pages picks up the complete sprite.
		err = c.emitPages(a)
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
	AssetsURLPrefix string // New: for HTTP routes
	DevMode         bool   // If true, disables caching (default: false)
	SiteURL         string // Optional: canonical base URL (e.g. "https://example.com"), used for sitemap.xml and canonical URL resolution
}

func NewAssetMin(ac *Config) *AssetMin {
	c := &AssetMin{
		Config:           ac,
		min:              minify.New(),
		minifyEnabled:    true,
		standaloneJS:     make(map[string]*asset),
		standaloneOwners: make(map[string][]string),
		moduleSprites:    make(map[string]*sprite.Sprite),
		fs:               NewOsFS(),
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
	c.min.AddFunc("application/xml", xml.Minify)
	c.min.AddFunc("text/xml", xml.Minify)

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
func (c *AssetMin) Read(p string) ([]byte, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	urlKey := p
	if !strings.HasPrefix(urlKey, "/") {
		urlKey = "/" + urlKey
	}

	outDir := ""
	if c.Config != nil {
		outDir = c.Config.OutputDir
	}

	for _, a := range c.allAssets {
		cleanURL := urlKey
		if strings.HasSuffix(cleanURL, "/") && cleanURL != "/" {
			cleanURL = strings.TrimRight(cleanURL, "/")
		}
		aURL := a.GetURLPath()
		if strings.HasSuffix(aURL, "/") && aURL != "/" {
			aURL = strings.TrimRight(aURL, "/")
		}

		if a.GetURLPath() == urlKey || aURL == cleanURL || a.outputPath == p ||
			(a.GetURLPath() == "/" && (urlKey == "/index.html" || (outDir != "" && p == filepath.Join(outDir, "index.html")))) ||
			(strings.HasSuffix(urlKey, "/") && urlKey != "/" && (a.GetURLPath() == urlKey+"index.html" || (outDir != "" && a.outputPath == filepath.Join(outDir, strings.TrimPrefix(urlKey, "/")+"index.html")))) {
			content, err := a.GetMinifiedContent(c.activeMinifier())
			if err == nil {
				return content, a.mediatype, true
			}
		}
	}

	for i := len(c.directArtifacts) - 1; i >= 0; i-- {
		art := c.directArtifacts[i]
		if art.Path == urlKey || art.Path == p || (strings.HasSuffix(urlKey, "/") && art.Path == urlKey+"index.html") {
			return art.Content, art.Mediatype, true
		}
	}

	if c.fs != nil {
		clean := strings.TrimPrefix(p, "/")
		if content, mt, ok := c.fs.Read(clean); ok {
			return content, mt, true
		}
		if content, mt, ok := c.fs.Read(p); ok {
			return content, mt, true
		}
		if strings.HasSuffix(clean, "/") {
			if content, mt, ok := c.fs.Read(clean + "index.html"); ok {
				return content, mt, true
			}
		}
	}

	return nil, "", false
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

	urlKey := path.Join("/", outPath)
	c.directArtifacts = append(c.directArtifacts, Artifact{
		Path:      urlKey,
		Mediatype: mediatype,
		Content:   content,
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
