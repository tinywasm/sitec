package ssr

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tinywasm/fmt"
)

// ssrCacheEntry holds a cached extraction result keyed by module hash set.
type ssrCacheEntry struct {
	hashSet string // Combined hash of all module Go files
	results map[string]ssrCollectorOutput
}

// ssrCache manages content-hash based caching for SSR asset extraction.
type ssrCache struct {
	entries map[string]*ssrCacheEntry
}

// newSSRCache creates a new cache instance.
func newSSRCache() *ssrCache {
	return &ssrCache{
		entries: make(map[string]*ssrCacheEntry),
	}
}

// computeModuleHashSet computes a combined hash of all Go files in the module set.
func computeModuleHashSet(modules []module) (string, error) {
	var filePaths []string

	for _, m := range modules {
		if m.dir == "" {
			continue
		}

		// Walk the module directory and collect all .go files (excluding *_test.go)
		err := filepath.Walk(m.dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				filePaths = append(filePaths, path)
			}
			return nil
		})

		if err != nil {
			return "", fmt.Err("failed to walk module dir", m.dir, err)
		}
	}

	// Sort for deterministic order
	sort.Strings(filePaths)

	// Compute hash of all file contents
	h := md5.New()
	for _, filePath := range filePaths {
		f, err := os.Open(filePath)
		if err != nil {
			return "", fmt.Err("failed to open file", filePath, err)
		}
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return "", fmt.Err("failed to hash file", filePath, err)
		}
		f.Close()
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// get retrieves cached results if the module hash set matches.
func (c *ssrCache) get(hashSet string) (map[string]ssrCollectorOutput, bool) {
	entry, ok := c.entries[hashSet]
	if !ok {
		return nil, false
	}
	return entry.results, true
}

// set caches extraction results for a module hash set.
func (c *ssrCache) set(hashSet string, results map[string]ssrCollectorOutput) {
	c.entries[hashSet] = &ssrCacheEntry{
		hashSet: hashSet,
		results: results,
	}
}
