package ssr

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/tinywasm/assetmin"
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/js"
	"github.com/tinywasm/svg/sprite"
)

var reLayer = regexp.MustCompile(`@layer\s+([^;{]+);`)

// extractAssetsForModule is the internal implementation that takes a resolved module.
func extractAssetsForModule(m module, rootDir string, allModules []module, binCachePath string, cache *ssrCache, scanner *scanner, assetLibraries []string, log func(...any), mu *sync.Mutex) (*assetmin.SSRAssets, error) {
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
		// Do compile-and-invoke
		results, err := invokeSSRExtractorOnce(rootDir, modulesForExtract, scanner, assetLibraries)
		if err != nil {
			mu.Unlock()
			return nil, err
		}

		// Cache the results
		cache.set(hashKey, results)
		cachedResults = results
	}
	mu.Unlock()

	// Collect the results for the requested module AND for the packages inside it.
	// The assets of an app live in its packages (config/, modules/x/), never at the
	// module root — asking only for m.path returned nothing and the app shipped with
	// an empty stylesheet.
	output, ok, err := MergeResultsFor(m.path, cachedResults)
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

	return &assetmin.SSRAssets{
		ModuleName: m.path,
		RootCSS:    output.Root,
		CSS:        output.Render,
		JS:         scripts,
		HTML:       output.HTML,
		Icons:      output.Icons,
		Fonts:      output.Fonts,
	}, nil
}

// MergeResultsFor gathers the module's own assets plus those of every package under
// it, in a stable order so the emitted CSS does not shuffle between runs.
func MergeResultsFor(modulePath string, results map[string]CollectorOutput) (CollectorOutput, bool, error) {
	paths := make([]string, 0, len(results))
	for p := range results {
		if p == modulePath || strings.HasPrefix(p, modulePath+"/") {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return CollectorOutput{}, false, nil
	}
	sort.Strings(paths)

	// Step 4: Layer statement conflict check
	var allLayers []layerInfo
	for _, p := range paths {
		out := results[p]
		allLayers = append(allLayers, extractLayers(out.Root, p)...)
		allLayers = append(allLayers, extractLayers(out.Render, p)...)
	}

	if len(allLayers) > 0 {
		first := allLayers[0]
		for i := 1; i < len(allLayers); i++ {
			current := allLayers[i]
			if !layersEqual(first.layers, current.layers) {
				return CollectorOutput{}, false, fmt.Err("ssr: conflicting @layer order:",
					fmt.Sprintf("%s declares %s, %s declares %s", first.pkgPath, strings.TrimSuffix(first.statement, ";"), current.pkgPath, strings.TrimSuffix(current.statement, ";")))
			}
		}
	}

	var merged CollectorOutput
	merged.Icons = sprite.NewSprite()
	var fontsFrom string

	for _, p := range paths {
		out := results[p]
		merged.Root += out.Root
		merged.Render += out.Render
		merged.HTML += out.HTML
		merged.Scripts = append(merged.Scripts, out.Scripts...)
		if out.Icons != nil {
			merged.Icons.Merge(out.Icons)
		}
		if out.Fonts.Family() != "" {
			if fontsFrom != "" {
				return CollectorOutput{}, false, fmt.Err("ssr: multiple Fonts() declarations:",
					fontsFrom, "and", p, "— only one package per module may declare Fonts()")
			}
			merged.Fonts = out.Fonts
			fontsFrom = p
		}
	}

	return merged, true, nil
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

type layerInfo struct {
	pkgPath   string
	statement string
	layers    []string
}

func extractLayers(css string, pkgPath string) []layerInfo {
	matches := reLayer.FindAllStringSubmatch(css, -1)
	var infos []layerInfo
	for _, m := range matches {
		statement := m[0]
		layersRaw := m[1]
		layers := parseLayerList(layersRaw)
		infos = append(infos, layerInfo{
			pkgPath:   pkgPath,
			statement: statement,
			layers:    layers,
		})
	}
	return infos
}

func parseLayerList(s string) []string {
	parts := strings.Split(s, ",")
	var list []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			list = append(list, trimmed)
		}
	}
	return list
}

func layersEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
