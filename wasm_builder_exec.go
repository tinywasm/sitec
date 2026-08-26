package sitec

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/tinywasm/fmt"
	"github.com/tinywasm/js"
	"github.com/tinywasm/tinygo"
)

// WasmBuildOptions selects what to compile and what to call the result.
//
// The zero value builds a site frontend: web/client.go → client.wasm. Set the
// fields to compile something else — an edge worker entry point, for example,
// which is main.go and must come out named for the platform that serves it.
type WasmBuildOptions struct {
	// Entry is the input file relative to the directory passed to Build, used to
	// locate the package directory to compile and verify its existence.
	// Empty means "web/client.go".
	Entry string
	// OutputName is the artifact name without the .wasm extension.
	// Empty means "client".
	OutputName string
}

func (o WasmBuildOptions) entry() string {
	if o.Entry == "" {
		return filepath.Join("web", "client.go")
	}
	return o.Entry
}

func (o WasmBuildOptions) filename() string {
	if o.OutputName == "" {
		return "client.wasm"
	}
	return o.OutputName + ".wasm"
}

type defaultWasmBuilder struct {
	stdlib bool // if true, use standard Go compiler instead of TinyGo
	opts   WasmBuildOptions
}

// NewDefaultWasmBuilder builds a site frontend from web/client.go.
func NewDefaultWasmBuilder(stdlib bool) WasmBuilder {
	return &defaultWasmBuilder{stdlib: stdlib}
}

// NewWasmBuilder builds an arbitrary entry point, for callers that are not
// compiling a site frontend.
func NewWasmBuilder(stdlib bool, opts WasmBuildOptions) WasmBuilder {
	return &defaultWasmBuilder{stdlib: stdlib, opts: opts}
}

func (w *defaultWasmBuilder) Build(dir string) (WasmOutput, error) {
	entry := w.opts.entry()
	clientPath := filepath.Join(dir, entry)
	if _, err := os.Stat(clientPath); err != nil {
		return WasmOutput{}, fmt.Err("input file not found: ", entry, " must exist")
	}
	pkgDir := filepath.Join(dir, filepath.Dir(entry))

	env := os.Environ()

	if !w.stdlib {
		// Ensure tinygo is installed
		if _, err := tinygo.EnsureInstalled(); err != nil {
			return WasmOutput{}, fmt.Err("cannot install TinyGo: ", err)
		}
		// Use tinygo.GetEnv() to get the perfect environment including TINYGOROOT and PATH
		env = tinygo.GetEnv()
		env = append(env, "GOOS=js", "GOARCH=wasm")
	} else {
		env = append(env, "GOOS=js", "GOARCH=wasm")
	}

	// Output filename
	wasmFilename := w.opts.filename()

	// Create temp output path for compilation
	tmpOutDir, err := os.MkdirTemp("", "sitec_wasm_*")
	if err != nil {
		return WasmOutput{}, err
	}
	defer os.RemoveAll(tmpOutDir)
	tmpOutPath := filepath.Join(tmpOutDir, wasmFilename)

	// Paso 5: compile
	//
	// El compilador corre con cmd.Dir = pkgDir para compilar el paquete entero
	// (el directorio) en vez de un archivo suelto. Esto permite que el paquete
	// tenga archivos hermanos (ej: main.go y access.go).
	var cmd *exec.Cmd
	if !w.stdlib {
		// -no-debug: a shipped web binary is never a source-level debug
		// target — DWARF info alone is the difference between ~99 KiB and
		// ~456 KiB for a minimal client, which is most of a size budget spent
		// on symbols nobody attaches a debugger to.
		cmd = exec.Command("tinygo", "build", "-target", "wasm", "-no-debug", "-o", tmpOutPath, ".")
	} else {
		cmd = exec.Command("go", "build", "-o", tmpOutPath, ".")
	}
	cmd.Dir = pkgDir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return WasmOutput{}, fmt.Err("compilation failed: ", string(out), err)
	}

	// Read compiled binary
	binary, err := os.ReadFile(tmpOutPath)
	if err != nil {
		return WasmOutput{}, err
	}

	// Paso 4: generar el runtime JS
	if !w.stdlib {
		js.SetRuntime(js.RuntimeTinyGo)
	} else {
		js.SetRuntime(js.RuntimeGo)
	}
	runtimeJS := js.PageBootstrap().Content

	return WasmOutput{
		Binary:   binary,
		Filename: wasmFilename,
		Runtime:  runtimeJS,
	}, nil
}
