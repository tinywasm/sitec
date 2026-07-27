//go:build !wasm

package ssr_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNegativeCompilation_RenderCSS(t *testing.T) {
	root := t.TempDir()

	// Write go.mod
	gomod := `module example.com/negtest

go 1.24

require (
	github.com/tinywasm/widget v0.1.0
	github.com/tinywasm/ssr v0.0.0
)

replace github.com/tinywasm/ssr => ../..
`
	// Locate the absolute path of the local tinywasm/ssr module
	absRoot, err := filepath.Abs("../")
	if err != nil {
		t.Fatal(err)
	}
	gomod = strings.ReplaceAll(gomod, "../..", absRoot)

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	// Write main.go that imports ssr and tries to pass a non-widget to Collect
	mainContent := `package main

import (
	"github.com/tinywasm/ssr"
)

type BadWidget struct{}

// It only has a loose RenderCSS method, doesn't satisfy widget.Widget or style.Styler
func (b *BadWidget) RenderCSS() string {
	return ".bad {}"
}

func main() {
	bad := &BadWidget{}
	// This MUST fail compilation because bad does not satisfy widget.Widget!
	_ = ssr.Collect(bad)
}
`
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Tidy the module first to resolve dependencies correctly
	cmdTidy := exec.Command("go", "mod", "tidy")
	cmdTidy.Dir = root
	if out, err := cmdTidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\nOutput: %s", err, string(out))
	}

	// Run go build
	cmd := exec.Command("go", "build", "main.go")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("EXPECTED COMPILATION FAILURE, but it compiled successfully! Output:\n%s", string(out))
	}

	t.Logf("Compilation failed as expected with error:\n%s", string(out))

	// Verify that the error is due to type mismatch on Collect parameter
	if !strings.Contains(string(out), "cannot use bad") && !strings.Contains(string(out), "cannot use &BadWidget{}") && !strings.Contains(string(out), "widget.Widget") {
		t.Fatalf("Unexpected compilation error: %s", string(out))
	}
}
