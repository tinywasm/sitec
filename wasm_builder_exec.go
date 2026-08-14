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

	// Paso 1 & 5: Ensure tinygo installed and get env
	var compilerPath string
	var err error
	env := os.Environ()

	if !w.stdlib {
		// TinyGo compilation
		compilerPath, err = tinygo.EnsureInstalled()
		if err != nil {
			return WasmOutput{}, fmt.Err("cannot install TinyGo: ", err)
		}
		binDir := filepath.Dir(compilerPath)
		// Add binDir to PATH
		if current := os.Getenv("PATH"); current != "" {
			os.Setenv("PATH", current+string(os.PathListSeparator)+binDir)
		} else {
			os.Setenv("PATH", binDir)
		}

		// Inject TINYGOROOT
		p, err := tinygo.GetPath()
		if err == nil {
			tinygoRoot := filepath.Dir(filepath.Dir(p))
			env = append(env, "TINYGOROOT="+tinygoRoot, "PATH="+os.Getenv("PATH"))
		}
		env = append(env, "GOOS=js", "GOARCH=wasm")
	} else {
		// Standard Go compilation
		compilerPath = "go"
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
