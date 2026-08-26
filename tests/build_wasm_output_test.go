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

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module testapp\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}

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

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module testapp\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}

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

// TestWasmBuild_CustomEntryAndOutputName covers the non-frontend case: an edge
// worker compiles main.go and must come out named for the platform that serves
// it, not "client.wasm". Without this, a caller like tinywasm/goflare cannot
// use this builder at all and has to keep its own copy of the compile step.
func TestWasmBuild_CustomEntryAndOutputName(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module testapp\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"),
		[]byte("package main\nfunc main() {}"), 0644); err != nil {
		t.Fatal(err)
	}

	wb := sitec.NewWasmBuilder(true, sitec.WasmBuildOptions{
		Entry:      "main.go",
		OutputName: "edge",
	})
	out, err := wb.Build(tmpDir)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if out.Filename != "edge.wasm" {
		t.Errorf("Filename = %q, want %q", out.Filename, "edge.wasm")
	}
	if len(out.Binary) == 0 {
		t.Error("Binary is empty")
	}
	if out.Runtime == "" {
		t.Error("Runtime is empty")
	}
}

// TestWasmBuild_DefaultsUnchanged pins the zero-value behaviour, so adding the
// options above cannot silently move the frontend path.
func TestWasmBuild_DefaultsUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	wb := sitec.NewWasmBuilder(true, sitec.WasmBuildOptions{})
	_, err := wb.Build(tmpDir)
	if err == nil || !strings.Contains(err.Error(), "web/client.go") {
		t.Errorf("zero options should still look for web/client.go, got: %v", err)
	}
}

// El builder compila con cmd.Dir = dir, asi que el entry viaja relativo a ese
// directorio. Con un dir relativo distinto de "." el bug era invisible en los
// otros tests —que usan t.TempDir(), absoluto— y hacia que el compilador
// buscara dir/dir/entry.
func TestWasmBuild_RelativeDirResolvesEntry(t *testing.T) {
	t.Chdir(t.TempDir())

	const projectDir = "proj"
	if err := os.MkdirAll(filepath.Join(projectDir, "web"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module testapp\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "web", "client.go"), []byte("package main\nfunc main() {}"), 0644); err != nil {
		t.Fatal(err)
	}

	wb := sitec.NewDefaultWasmBuilder(true) // stdlib: no requiere TinyGo
	out, err := wb.Build(projectDir)
	if err != nil {
		t.Fatalf("Build con dir relativo fallo: %v", err)
	}
	if len(out.Binary) == 0 {
		t.Error("expected non-empty binary")
	}
}

// TestWasmBuild_CompilesSiblingFilesInSamePackage cierra la regresion real de
// goflare-demo: un entry point con mas de un archivo en el mismo paquete
// perdia los archivos hermanos porque el compilador se invocaba con el
// nombre de archivo (modo command-line-arguments) en vez del directorio.
func TestWasmBuild_CompilesSiblingFilesInSamePackage(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module testapp\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "edge"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "edge", "main.go"),
		[]byte("package main\nfunc main() { helper() }"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "edge", "access.go"),
		[]byte("package main\nfunc helper() {}"), 0644); err != nil {
		t.Fatal(err)
	}

	wb := sitec.NewWasmBuilder(true, sitec.WasmBuildOptions{
		Entry:      "edge/main.go",
		OutputName: "edge",
	})
	out, err := wb.Build(tmpDir)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(out.Binary) == 0 {
		t.Error("Binary is empty")
	}
}
