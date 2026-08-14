package sitec_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinywasm/sitec"
)

func TestRunWasmBuild_FailsIfInputMissing(t *testing.T) {
	tmpDir := t.TempDir()

	// Run without web/client.go
	wb := sitec.NewDefaultWasmBuilder(true)
	_, err := wb.Build(tmpDir)
	if err == nil {
		t.Error("expected error when input file is missing, got nil")
	}
	if !strings.Contains(err.Error(), "input file not found") {
		t.Errorf("expected 'input file not found' error, got: %v", err)
	}
}

func TestWasmbuild_WritesScriptJSFromJSPackage_Stdlib(t *testing.T) {
	tmpDir := t.TempDir()

	// Create web/client.go
	if err := os.MkdirAll(filepath.Join(tmpDir, "web"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "web", "client.go"), []byte("package main\nfunc main() {}"), 0644); err != nil {
		t.Fatal(err)
	}

	wb := sitec.NewDefaultWasmBuilder(true) // Stdlib = true
	out, err := wb.Build(tmpDir)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if out.Filename != "client.wasm" {
		t.Errorf("expected filename 'client.wasm', got %q", out.Filename)
	}

	if len(out.Binary) == 0 {
		t.Error("expected non-empty binary")
	}

	// Check for Go signatures in the runtime/glue JS
	found := false
	goSigs := []string{"runtime.scheduleTimeoutEvent", "runtime.clearTimeoutEvent"}
	for _, sig := range goSigs {
		if strings.Contains(out.Runtime, sig) {
			found = true
			break
		}
	}
	if !found {
		t.Error("Runtime does not contain Go signatures")
	}
}

func TestWasmbuild_WritesScriptJSFromJSPackage_TinyGo(t *testing.T) {
	tmpDir := t.TempDir()

	// Create web/client.go
	if err := os.MkdirAll(filepath.Join(tmpDir, "web"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "web", "client.go"), []byte("package main\nfunc main() {}"), 0644); err != nil {
		t.Fatal(err)
	}

	wb := sitec.NewDefaultWasmBuilder(false) // Stdlib = false (TinyGo)
	out, err := wb.Build(tmpDir)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if out.Filename != "client.wasm" {
		t.Errorf("expected filename 'client.wasm', got %q", out.Filename)
	}

	if len(out.Binary) == 0 {
		t.Error("expected non-empty binary")
	}

	// Check for TinyGo signatures in the runtime/glue JS
	found := false
	tinygoSigs := []string{"runtime.sleepTicks", "runtime.ticks", "tinygo_js"}
	for _, sig := range tinygoSigs {
		if strings.Contains(out.Runtime, sig) {
			found = true
			break
		}
	}
	if !found {
		t.Error("Runtime does not contain TinyGo signatures")
	}
}
