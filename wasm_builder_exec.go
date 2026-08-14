package sitec

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/tinywasm/fmt"
	"github.com/tinywasm/js"
	"github.com/tinywasm/tinygo"
)

type defaultWasmBuilder struct {
	stdlib bool // if true, use standard Go compiler instead of TinyGo
}

func NewDefaultWasmBuilder(stdlib bool) WasmBuilder {
	return &defaultWasmBuilder{stdlib: stdlib}
}

func (w *defaultWasmBuilder) Build(dir string) (WasmOutput, error) {
	// Paso 2: verificar web/client.go
	clientPath := filepath.Join(dir, "web", "client.go")
	if _, err := os.Stat(clientPath); err != nil {
		return WasmOutput{}, fmt.Err("input file not found: web/client.go must exist")
	}

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
	wasmFilename := "client.wasm"

	// Create temp output path for compilation
	tmpOutDir, err := os.MkdirTemp("", "sitec_wasm_*")
	if err != nil {
		return WasmOutput{}, err
	}
	defer os.RemoveAll(tmpOutDir)
	tmpOutPath := filepath.Join(tmpOutDir, wasmFilename)

	// Paso 5: compile
	var cmd *exec.Cmd
	if !w.stdlib {
		cmd = exec.Command("tinygo", "build", "-target", "wasm", "-o", tmpOutPath, clientPath)
	} else {
		cmd = exec.Command("go", "build", "-o", tmpOutPath, clientPath)
	}
	cmd.Dir = dir
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
