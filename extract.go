package ssr

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/tinywasm/assetmin"
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/js"
)

var ssrSourceFiles = []string{"css.go", "js.go", "svg.go", "html.go", "ssr.go"}

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
		// Do compile-and-invoke
		results, err := invokeSSRExtractorOnce(rootDir, modulesForExtract)
		if err != nil {
			mu.Unlock()
			return nil, err
		}

		// Cache the results
		cache.set(hashKey, results)
		cachedResults = results
	}
	mu.Unlock()

	// Extract the SSRAssets for the requested module
	output, ok := cachedResults[m.path]
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
	}, nil
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

// discoverModules discovers all modules in the project using go list.
func discoverModules(rootDir string) ([]module, error) {
	cmd := exec.Command("go", "list", "-m", "-json", "all")
	cmd.Dir = rootDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Err("go list failed", err)
	}

	var modules []module
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var m struct {
			Path string
			Dir  string
		}
		if err := dec.Decode(&m); err == nil && m.Dir != "" {
			modules = append(modules, module{path: m.Path, dir: m.Dir})
		}
	}

	return modules, nil
}
