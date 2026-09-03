//go:build !wasm

package sitec

import (
	"errors"
	"io"
	"testing"

	"github.com/tdewolff/minify/v2"
)

// This file lives at the module root, not under tests/, DELIBERATELY: every
// other *_test.go in this repo is package sitec_test (black-box) under
// tests/, but this one is white-box (package sitec) because it needs
// AssetMin's unexported fields (mu, allAssets) and the unexported asset
// struct. This is the one intentional exception to the tests/ convention in
// this repo; do not "fix" it by moving it there — package sitec inside
// tests/ would be a different, disconnected package with no access to these
// fields at all.
//
// FlushToDisk used to discard RegenerateCache's error (emit_flush.go, before
// this fix), silently leaving the asset's cache empty and writing a 0-byte
// file to disk. Reproducing a real minifier error through the public API is
// impractical — tdewolff's css/js/svg/xml minifiers are built to tolerate
// malformed input rather than fail — so this test registers a minifier for a
// fabricated mediatype that deliberately errors, and inserts the asset
// directly. That's what forces the white-box access above.
func TestFlushToDisk_PropagatesRegenerateCacheError(t *testing.T) {
	const boomMediatype = "text/x-flush-test-error"
	boom := errors.New("boom: minifier rejected this asset")

	am := NewAssetMin(&Config{OutputDir: t.TempDir()})
	am.min.AddFunc(boomMediatype, func(_ *minify.M, _ io.Writer, _ io.Reader, _ map[string]string) error {
		return boom
	})

	am.mu.Lock()
	am.allAssets["broken.txt"] = &asset{
		fileOutputName: "broken.txt",
		outputPath:     "broken.txt",
		mediatype:      boomMediatype,
		initCode:       func() (string, error) { return "irrelevant content", nil },
	}
	am.mu.Unlock()

	err := am.FlushToDisk()
	if err == nil {
		t.Fatal("expected FlushToDisk to return the RegenerateCache error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("expected the error chain to contain the minifier's error, got: %v", err)
	}
	if am.DiskMirrored() {
		t.Error("DiskMirrored must stay false when FlushToDisk fails")
	}
}

func TestDiskMirrored_FalseBeforeFlush(t *testing.T) {
	am := NewAssetMin(&Config{OutputDir: t.TempDir()})
	if am.DiskMirrored() {
		t.Error("a fresh AssetMin must not report DiskMirrored before any FlushToDisk call")
	}
}
