package sitec

import (
	"os"
	"path/filepath"
	"sync"
)

// memFS is an in-memory implementation of FS
type memFS struct {
	mu        sync.RWMutex
	artifacts map[string]Artifact
}

func NewMemFS() FS {
	return &memFS{
		artifacts: make(map[string]Artifact),
	}
}

func (m *memFS) Write(path string, content []byte, mediatype string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.artifacts[path] = Artifact{
		Path:      path,
		Mediatype: mediatype,
		Content:   content,
	}
	return nil
}

func (m *memFS) Read(path string) ([]byte, string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	art, ok := m.artifacts[path]
	if !ok {
		return nil, "", false
	}
	return art.Content, art.Mediatype, true
}

func (m *memFS) List() []Artifact {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Artifact, 0, len(m.artifacts))
	for _, art := range m.artifacts {
		out = append(out, art)
	}
	return out
}

// osFS is an atomic disk-writing implementation of FS
type osFS struct {
	mu        sync.RWMutex
	artifacts map[string]Artifact
}

func NewOsFS() FS {
	return &osFS{
		artifacts: make(map[string]Artifact),
	}
}

func (o *osFS) Write(path string, content []byte, mediatype string) error {
	o.mu.Lock()
	o.artifacts[path] = Artifact{
		Path:      path,
		Mediatype: mediatype,
		Content:   content,
	}
	o.mu.Unlock()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Write atomically: write to temp file and rename
	tmpFile, err := os.CreateTemp(dir, "sitec_tmp_*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err := tmpFile.Write(content); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}

func (o *osFS) Read(path string) ([]byte, string, bool) {
	o.mu.RLock()
	art, ok := o.artifacts[path]
	o.mu.RUnlock()
	if ok {
		return art.Content, art.Mediatype, true
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, "", false
	}
	mt := "text/plain"
	switch filepath.Ext(path) {
	case ".css":
		mt = "text/css"
	case ".js":
		mt = "text/javascript"
	case ".svg":
		mt = "image/svg+xml"
	case ".html":
		mt = "text/html"
	}
	return content, mt, true
}

func (o *osFS) List() []Artifact {
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make([]Artifact, 0, len(o.artifacts))
	for _, art := range o.artifacts {
		out = append(out, art)
	}
	return out
}
