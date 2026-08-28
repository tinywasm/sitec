package sitec

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// EnableSSRMode activates the SSR event branch unconditionally. Pure setter.
func (c *AssetMin) EnableSSRMode() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ssrEnabled = true
}

// SetSSRCompiler registers a Go compiler callback. Pure setter — does NOT invoke fn.
// Pass nil to unregister.
func (c *AssetMin) SetSSRCompiler(fn func() error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onSSRCompile = fn
}

// artifactDiskPath translates an Artifact's Path URL ("/", "/img/x.webp",
// "/acerca/") to its path under OutputDir. An artifact's identity is its URL;
// disk is only a projection of it. Shared with Output.diskPath (build.go) —
// one formula, two consumers.
func (c *AssetMin) artifactDiskPath(urlPath string) string {
	rel := strings.TrimPrefix(urlPath, "/")
	if rel == "" {
		rel = "index.html"
	}
	if strings.HasSuffix(rel, "/") {
		rel = rel + "index.html"
	}
	return filepath.Join(c.OutputDir, rel)
}

// FlushToDisk snapshots all registered assets AND direct artifacts (images,
// the WASM binary — anything registered via Write instead of the minifier
// pipeline), writes them to disk (overwrite), and sets diskMirrored = true
// only if every write across both batches succeeds. Returns the first error.
func (c *AssetMin) FlushToDisk() error {
	type snapshot struct {
		path      string
		content   []byte
		mediatype string
	}

	c.mu.Lock()
	snapshots := make([]snapshot, 0, len(c.allAssets)+len(c.directArtifacts))

	var regenErr error
	for _, a := range c.allAssets {
		if err := a.RegenerateCache(c.activeMinifier()); err != nil && regenErr == nil {
			regenErr = fmt.Errorf("FlushToDisk: regenerating %s: %w", a.outputPath, err)
			continue
		}
		snapshots = append(snapshots, snapshot{
			path:      a.outputPath,
			content:   a.GetCachedMinified(),
			mediatype: a.mediatype,
		})
	}
	for _, art := range c.directArtifacts {
		snapshots = append(snapshots, snapshot{
			path:      c.artifactDiskPath(art.Path),
			content:   art.Content,
			mediatype: art.Mediatype,
		})
	}
	c.mu.Unlock()

	if regenErr != nil {
		return regenErr
	}

	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].path < snapshots[j].path })

	for _, s := range snapshots {
		if err := c.fs.Write(s.path, s.content, s.mediatype); err != nil {
			return fmt.Errorf("FlushToDisk %s: %w", s.path, err)
		}
	}

	c.mu.Lock()
	c.diskMirrored = true
	c.mu.Unlock()
	return nil
}

// DiskMirrored reports whether FlushToDisk has already succeeded, and
// therefore every subsequent asset regeneration is being mirrored to disk.
func (c *AssetMin) DiskMirrored() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.diskMirrored
}

// isSSRMode returns true if the package is being used as a dependency (SSR mode).
// It assumes the caller holds c.mu.
func (c *AssetMin) isSSRMode() bool {
	return c.ssrEnabled
}
