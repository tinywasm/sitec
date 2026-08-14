//go:build !wasm

package sitec_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/tinywasm/sitec"
)

// AssetMin.Write registers a pre-built artifact (e.g. a compiled WASM binary)
// for the sink. Before this test, Write() routed the content through the same
// ContentFile-assembly + minifier pipeline used for CSS/JS/HTML fragments:
//   - WriteContent appends "\n" after every fragment, corrupting a binary.
//   - No minifier is registered for arbitrary binary mediatypes (only
//     text/css, javascript, image/svg+xml, text/html are), so
//     minifier.Bytes returned minify.ErrNotExist.
//   - FlushToDisk's loop discarded that error, leaving the asset's cache
//     empty and the file written to disk at 0 bytes — silently, with exit
//     code 0 and "status": "success" in the CLI manifest.
//
// This test writes binary content indistinguishable from a real .wasm module
// (arbitrary bytes, including a trailing null byte a "\n"-joiner would not
// preserve as-is) and asserts it survives Write + FlushToDisk unmodified.
func TestAssetMin_WriteBinaryContentSurvivesFlush(t *testing.T) {
	outDir := t.TempDir()
	am := sitec.NewAssetMin(&sitec.Config{OutputDir: outDir, RootDir: outDir})
	am.SetFS(sitec.NewOsFS())

	content := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0xff, 0x00}

	if err := am.Write("client.wasm", content, "application/wasm"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := am.FlushToDisk(); err != nil {
		t.Fatalf("FlushToDisk: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outDir, "client.wasm"))
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content corrupted: wrote %d bytes %x, got %d bytes %x", len(content), content, len(got), got)
	}
}

// The written artifact must appear in List() with a proper URL path, so the
// CLI manifest and the dev-mode `serve` package (which enumerates fs.List())
// both see it.
func TestAssetMin_WriteRegistersArtifactInList(t *testing.T) {
	am := sitec.NewAssetMin(&sitec.Config{OutputDir: t.TempDir()})
	am.SetFS(sitec.NewMemFS())

	if err := am.Write("client.wasm", []byte{0x00, 0x61, 0x73, 0x6d}, "application/wasm"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var found bool
	for _, a := range am.List() {
		if a.Path == "/client.wasm" && a.Mediatype == "application/wasm" {
			found = true
		}
	}
	if !found {
		t.Errorf("List() does not contain the written artifact: %+v", am.List())
	}
}
