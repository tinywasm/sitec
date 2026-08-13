package ssr

import (
	"os"
	"strings"
	"sync"

	"github.com/tinywasm/assetmin"
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/js"
	"github.com/tinywasm/modfind"
)

const (
	cssModulePath            = "tinywasm/css"
	noAssetLibrariesWarning = "ssr: no asset libraries configured; packages that import a styling library " +
		"and declare no producer will NOT fail the build (see SetAssetLibraries)"
	errNoAssetsExtracted     = "ssr: no assets extracted from any module; the stylesheet would be empty"
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
	mu             sync.Mutex
	lister         GraphLister
	warnOnce       *sync.Once
}

func New(rootDir string) *Extractor {
	return &Extractor{
		rootDir:        rootDir,
		log:            func(...any) {},
		cache:          newSSRCache(),
		scanner:        newScanner(),
		AssetLibraries: []string{},
		lister:         goListDeps,
		warnOnce:       &sync.Once{},
	}
}

func (e *Extractor) SetLog(fn func(...any))     { e.log = fn }
func (e *Extractor) SetFinder(f *modfind.Finder) { e.finder = f }
func (e *Extractor) SetLister(lister GraphLister) { e.lister = lister }

func (e *Extractor) SetAssetLibraries(libs []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.AssetLibraries = libs
	e.warnOnce = &sync.Once{}
}

func (e *Extractor) results(rootDir string, modules []module) (map[string]CollectorOutput, error) {
	hashKey, err := computeModuleHashSet(modules)
	if err != nil {
		return nil, fmt.Err("failed to compute module hash", err)
	}

	e.mu.Lock()
	cachedResults, hasCached := e.cache.get(hashKey)
	if !hasCached {
		results, err := invokeSSRExtractorOnce(rootDir, modules, e.scanner, e.AssetLibraries, e.lister, e.log)
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

func (e *Extractor) ExtractModule(moduleDir string) (*assetmin.SSRAssets, error) {
	e.mu.Lock()
	if len(e.AssetLibraries) == 0 {
		e.warnOnce.Do(func() {
			e.log(noAssetLibrariesWarning)
		})
	}
	e.mu.Unlock()

	rootDir, err := findProjectRoot(moduleDir)
	if err != nil {
		return nil, fmt.Err("find project root:", err)
	}
	modules, err := e.discoverModules(rootDir)
	if err != nil {
		modules = []module{{path: moduleDir, dir: moduleDir}}
	}

	var target module
	for _, m := range modules {
		if m.dir == moduleDir {
			target = m
			break
		}
	}

	if target.dir == "" {
		// resolve to the containing module for subpackages
		for _, m := range modules {
			if strings.HasPrefix(moduleDir, m.dir+string(os.PathSeparator)) {
				target = m
				break
			}
		}

		if target.dir == "" {
			target = module{path: moduleDir, dir: moduleDir}
		}
	}

	modulesForExtract := modules
	if !containsModule(modules, target) {
		modulesForExtract = append(append([]module(nil), modules...), target)
	}

	cachedResults, err := e.results(rootDir, modulesForExtract)
	if err != nil {
		return nil, err
	}

	output, ok, err := MergeResultsFor(target.path, cachedResults)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	scripts := make([]*js.Script, 0, len(output.Scripts))
	for _, s := range output.Scripts {
		scripts = append(scripts, &js.Script{
			Name:    s.Name,
			Content: s.Content,
		})
	}

	a := &assetmin.SSRAssets{
		ModuleName: target.path,
		RootCSS:    output.Root,
		CSS:        output.Render,
		JS:         scripts,
		HTML:       output.HTML,
		Icons:      output.Icons,
		Fonts:      output.Fonts,
	}

	a.IsRoot = isRootDir(target.dir, e.rootDir)
	a.IsFramework = isFrameworkModule(target.path)
	return a, nil
}

func (e *Extractor) ExtractAll() ([]*assetmin.SSRAssets, error) {
	e.mu.Lock()
	if len(e.AssetLibraries) == 0 {
		e.warnOnce.Do(func() {
			e.log(noAssetLibrariesWarning)
		})
	}
	e.mu.Unlock()

	modules, err := e.discoverModules(e.rootDir)
	if err != nil {
		return nil, err
	}

	cachedResults, err := e.results(e.rootDir, modules)
	if err != nil {
		return nil, err
	}

	var all []*assetmin.SSRAssets
	for _, m := range modules {
		output, ok, err := MergeResultsFor(m.path, cachedResults)
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

			a := &assetmin.SSRAssets{
				ModuleName: m.path,
				RootCSS:    output.Root,
				CSS:        output.Render,
				JS:         scripts,
				HTML:       output.HTML,
				Icons:      output.Icons,
				Fonts:      output.Fonts,
			}

			a.IsRoot = isRootDir(m.dir, e.rootDir)
			a.IsFramework = isFrameworkModule(m.path)
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

func isRootDir(dir, rootDir string) bool {
	if rootDir == "" {
		return false
	}
	return dir == rootDir
}

func isFrameworkModule(path string) bool {
	return path == cssModulePath || strings.HasSuffix(path, "/"+cssModulePath)
}
