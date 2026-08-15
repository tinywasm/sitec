package sitec

import (
	"os"
	"strings"
	"sync"

	"github.com/tinywasm/fmt"
	"github.com/tinywasm/js"
	"github.com/tinywasm/modfind"
)

const (
	cssModulePath           = "tinywasm/css"
	noAssetLibrariesWarning = "ssr: no asset libraries configured; packages that import a styling library " +
		"and declare no producer will NOT fail the build (see SetAssetLibraries)"
	errNoAssetsExtracted    = "sitec: ningún módulo produjo assets; la hoja de estilos saldría vacía"
)

type module struct {
	path string
	dir  string
}

type Extractor struct {
	rootDir        string
	finder         *modfind.Finder
	log            func(...any)
	cache          *ssrCache
	scanner        *scanner
	AssetLibraries []string
	lister         GraphLister
	warnOnce       *sync.Once
	toolchain      Toolchain
	wasmBuilder    WasmBuilder
	mu             sync.Mutex
}

func New(rootDir string) *Extractor {
	return &Extractor{
		rootDir:        rootDir,
		log:            func(...any) {},
		cache:          newSSRCache(),
		scanner:        newScanner(),
		AssetLibraries: []string{},
		warnOnce:       &sync.Once{},
		toolchain:      NewExecToolchain(),
	}
}

func (e *Extractor) SetLog(fn func(...any))          { e.log = fn }
func (e *Extractor) SetFinder(f *modfind.Finder)     { e.finder = f }
func (e *Extractor) SetGraphLister(l GraphLister)    { e.lister = l }
func (e *Extractor) SetToolchain(t Toolchain)        { e.toolchain = t }
func (e *Extractor) SetWasmBuilder(wb WasmBuilder)    { e.wasmBuilder = wb }

func (e *Extractor) Finder() *modfind.Finder {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.finder == nil {
		e.finder = modfind.New()
	}
	return e.finder
}

func (e *Extractor) WasmBuilder() WasmBuilder {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.wasmBuilder
}

func (e *Extractor) SetAssetLibraries(libs []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.AssetLibraries = libs
	e.warnOnce = &sync.Once{}
}

func (e *Extractor) defaultLister(rootDir, pattern, goos, goarch string) ([]string, error) {
	env := []string{}
	if goos != "" {
		env = append(env, "GOOS="+goos)
	}
	if goarch != "" {
		env = append(env, "GOARCH="+goarch)
	}

	var data []byte
	var err error
	if len(env) > 0 {
		data, err = e.toolchain.ListEnv(rootDir, env, "-e", "-deps", pattern)
	} else {
		data, err = e.toolchain.List(rootDir, "-e", "-deps", pattern)
	}
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, nil
}

func (e *Extractor) results(projectRoot string, startDir string, modules []module) (map[string]CollectorOutput, error) {
	hashKey, err := computeModuleHashSet(modules)
	if err != nil {
		return nil, fmt.Err("failed to compute module hash", err)
	}

	e.mu.Lock()
	cachedResults, hasCached := e.cache.get(hashKey)
	if !hasCached {
		l := e.lister
		if l == nil {
			l = e.defaultLister
		}
		results, err := invokeSSRExtractorOnce(projectRoot, startDir, modules, e.scanner, e.AssetLibraries, l, e.log, e.toolchain)
		if err != nil {
			e.mu.Unlock()
			return nil, err
		}

		e.cache.set(hashKey, results)
		cachedResults = results
	}
	e.mu.Unlock()

	return cachedResults, nil
}

func (e *Extractor) ExtractModule(moduleDir string) (*Assets, error) {
	e.mu.Lock()
	warnOnce := e.warnOnce
	e.mu.Unlock()

	if len(e.AssetLibraries) == 0 && warnOnce != nil {
		warnOnce.Do(func() {
			e.log(noAssetLibrariesWarning)
		})
	}

	rootDir, err := findProjectRoot(moduleDir)
	if err != nil {
		return nil, fmt.Err("find project root:", err)
	}
	modules, err := e.discoverModules(rootDir)
	if err != nil {
		modules = []module{{path: moduleDir, dir: moduleDir}}
	}

	target := resolveOwningModule(moduleDir, modules)

	results, err := e.results(rootDir, e.rootDir, modules)
	if err != nil {
		return nil, err
	}

	output, ok, err := MergeResultsFor(target.path, results)
	if err != nil || !ok {
		return nil, err
	}

	scripts := make([]*js.Script, 0, len(output.Scripts))
	for _, s := range output.Scripts {
		scripts = append(scripts, &js.Script{
			Name:    s.Name,
			Content: s.Content,
		})
	}

	rootModule := resolveOwningModule(e.rootDir, modules)
	a := &Assets{
		ModuleName:  target.path,
		RootCSS:     output.Root,
		CSS:         output.Render,
		JS:          scripts,
		HTML:        output.HTML,
		Icons:       output.Icons,
		Fonts:       output.Fonts,
		IsRoot:      target.path == rootModule.path,
		IsFramework: isFrameworkModule(target.path),
	}
	return a, nil
}

func (e *Extractor) ExtractAll() ([]*Assets, error) {
	e.mu.Lock()
	warnOnce := e.warnOnce
	e.mu.Unlock()

	if len(e.AssetLibraries) == 0 && warnOnce != nil {
		warnOnce.Do(func() {
			e.log(noAssetLibrariesWarning)
		})
	}

	modules, err := e.discoverModules(e.rootDir)
	if err != nil {
		return nil, err
	}

	rootDir, err := findProjectRoot(e.rootDir)
	if err != nil {
		rootDir = e.rootDir
	}

	results, err := e.results(rootDir, e.rootDir, modules)
	if err != nil {
		return nil, err
	}

	rootModule := resolveOwningModule(e.rootDir, modules)

	var all []*Assets
	for _, m := range modules {
		output, ok, err := MergeResultsFor(m.path, results)
		if err != nil {
			return nil, err
		}
		if ok {
			scripts := make([]*js.Script, 0, len(output.Scripts))
			for _, s := range output.Scripts {
				scripts = append(scripts, &js.Script{
					Name:    s.Name,
					Content: s.Content,
				})
			}
			a := &Assets{
				ModuleName:  m.path,
				RootCSS:     output.Root,
				CSS:         output.Render,
				JS:          scripts,
				HTML:        output.HTML,
				Icons:       output.Icons,
				Fonts:       output.Fonts,
				IsRoot:      m.path == rootModule.path,
				IsFramework: isFrameworkModule(m.path),
			}
			all = append(all, a)
		}
	}

	if len(all) == 0 {
		return nil, fmt.Err(errNoAssetsExtracted)
	}
	return all, nil
}

func (e *Extractor) discoverModules(rootDir string) ([]module, error) {
	if e.finder == nil {
		e.finder = modfind.New()
	}
	found, err := e.finder.Discover(rootDir)
	if err != nil {
		return nil, err
	}
	var mods []module
	for _, m := range found {
		mods = append(mods, module{path: m.Path, dir: m.Dir})
	}
	return mods, nil
}

func isFrameworkModule(path string) bool {
	return path == cssModulePath || strings.HasSuffix(path, "/"+cssModulePath)
}

// resolveOwningModule finds which discovered module a directory belongs to:
// an exact match first, then the nearest ancestor by path prefix — a site
// subdirectory with no go.mod of its own (e.g. sites/a/ inside a shared
// module used to build several similarly-themed sites from one CI/CD
// pipeline) resolves to that shared module, exactly like `go list -m` does
// when run from within it. Falls back to a synthetic module{path: dir, dir:
// dir} so callers never see an empty path.
func resolveOwningModule(dir string, modules []module) module {
	for _, m := range modules {
		if m.dir == dir {
			return m
		}
	}
	for _, m := range modules {
		if strings.HasPrefix(dir, m.dir+string(os.PathSeparator)) {
			return m
		}
	}
	return module{path: dir, dir: dir}
}

// ValidateProject checks if the given directory contains a valid sitec project.
func ValidateProject(dir string) error {
	_, err := findProjectRoot(dir)
	if err != nil {
		return fmt.Err("invalid project: " + err.Error())
	}
	return nil
}
