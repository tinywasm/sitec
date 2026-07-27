package ssr

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/tinywasm/assetmin"
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/js"
	"github.com/tinywasm/svg/sprite"
)

var ssrSourceFiles = []string{"css.go", "js.go", "svg.go", "html.go"}

// extractAssetsForModule is the internal implementation that takes a resolved module.
func extractAssetsForModule(m module, rootDir string, allModules []module, binCachePath string, cache *ssrCache, log func(...any), mu *sync.Mutex) (*assetmin.SSRAssets, error) {
	// Ensure m is in the extractor's module set, so the generated main.go
	// imports it and the results map carries an entry for m.path.
	modulesForExtract := allModules
	if !containsModule(allModules, m) {
		modulesForExtract = append(append([]module(nil), allModules...), m)
	}

	// Compute hash of all modules to check global cache
	hashKey, err := computeModuleHashSet(modulesForExtract)
	if err != nil {
		return nil, fmt.Err("failed to compute module hash", err)
	}

	// Check cache
	mu.Lock()
	cachedResults, hasCached := cache.get(hashKey)
	if !hasCached {
		// Optimization: if there are absolutely no packages with SSR sources,
		// we don't compile/run anything. We just return an empty map.
		if len(expandToSSRPackages(modulesForExtract)) == 0 {
			cachedResults = make(map[string]Bundle)
		} else {
			// Do compile-and-invoke
			results, err := invokeSSRExtractorOnce(rootDir, modulesForExtract)
			if err != nil {
				mu.Unlock()
				return nil, err
			}
			cachedResults = results
		}

		// Cache the results
		cache.set(hashKey, cachedResults)
	}
	mu.Unlock()

	// Collect the results for the requested module AND for the packages inside it.
	// The assets of an app live in its packages (config/, modules/x/), never at the
	// module root — asking only for m.path returned nothing and the app shipped with
	// an empty stylesheet.
	output, ok := MergeResultsFor(m.path, cachedResults)
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

	return &assetmin.SSRAssets{
		ModuleName: m.path,
		RootCSS:    "", // RootCSS is now empty/unused, as all CSS is emitted as a unified cascade stylesheet in CSS
		CSS:        output.Render,
		JS:         scripts,
		HTML:       output.HTML,
		Icons:      output.Icons,
	}, nil
}

// MergeResultsFor gathers the module's own assets plus those of every package under
// it, in a stable order so the emitted CSS does not shuffle between runs.
func MergeResultsFor(modulePath string, results map[string]Bundle) (Bundle, bool) {
	paths := make([]string, 0, len(results))
	for p := range results {
		if p == modulePath || strings.HasPrefix(p, modulePath+"/") {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return Bundle{}, false
	}
	sort.Strings(paths)

	var merged Bundle
	merged.Icons = sprite.NewSprite()
	for _, p := range paths {
		out := results[p]
		merged.Render += out.Render
		merged.HTML += out.HTML
		merged.Scripts = append(merged.Scripts, out.Scripts...)
		merged.Icons.Merge(out.Icons)
	}
	return merged, true
}

// findProjectRoot finds the project root by locating the nearest go.mod file above or at startDir.
func findProjectRoot(startDir string) (string, error) {
	dir := startDir
	for {
		gomodPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(gomodPath); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Err("no go.mod found in", startDir, "or parent directories")
		}
		dir = parent
	}
}

func containsModule(mods []module, m module) bool {
	for _, x := range mods {
		if x.path == m.path && x.dir == m.dir {
			return true
		}
	}
	return false
}
