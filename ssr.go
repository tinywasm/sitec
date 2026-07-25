package ssr

import (
	"os"
	"strings"
	"sync"

	"github.com/tinywasm/assetmin"
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/modfind"
)

const cssModulePath = "tinywasm/css"

type module struct {
	path string
	dir  string
}

type Extractor struct {
	rootDir string
	finder  *modfind.Finder
	log     func(...any)
	cache   *ssrCache
	mu      sync.Mutex
}

func New(rootDir string) *Extractor {
	return &Extractor{
		rootDir: rootDir,
		log:     func(...any) {},
		cache:   newSSRCache(),
	}
}

func (e *Extractor) SetLog(fn func(...any))     { e.log = fn }
func (e *Extractor) SetFinder(f *modfind.Finder) { e.finder = f }

func (e *Extractor) ExtractModule(moduleDir string) (*assetmin.SSRAssets, error) {
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
	a, err := extractAssetsForModule(target, rootDir, modules, "", e.cache, e.log, &e.mu)
	if err != nil || a == nil {
		return nil, err
	}
	a.IsRoot = isRootDir(target.dir, e.rootDir)
	a.IsFramework = isFrameworkModule(target.path)
	return a, nil
}

func (e *Extractor) ExtractAll() ([]*assetmin.SSRAssets, error) {
	modules, err := e.discoverModules(e.rootDir)
	if err != nil {
		return nil, err
	}
	var all []*assetmin.SSRAssets
	for _, m := range modules {
		a, err := extractAssetsForModule(m, e.rootDir, modules, "", e.cache, e.log, &e.mu)
		if err != nil {
			e.log("ssr extract error:", m.path, err)
			continue
		}
		if a != nil {
			a.IsRoot = isRootDir(m.dir, e.rootDir)
			a.IsFramework = isFrameworkModule(m.path)
			all = append(all, a)
		}
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
