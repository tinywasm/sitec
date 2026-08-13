//go:build !wasm

package ssr_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tinywasm/modfind"
	"github.com/tinywasm/ssr"
)

func writeProj(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// fontModuleDir resolves the monorepo checkout of tinywasm/font (no network).
func fontModuleDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// tests/fonts_extract_test.go → ../../font
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "font")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(abs, "go.mod")); err != nil {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			gopath = filepath.Join(os.Getenv("HOME"), "go")
		}
		candidate := filepath.Join(gopath, "pkg", "mod", "github.com", "tinywasm", "font@v0.0.4")
		if _, err2 := os.Stat(filepath.Join(candidate, "go.mod")); err2 == nil {
			return candidate
		}
		t.Fatalf("font module not found at %s: %v", abs, err)
	}
	return abs
}

func writeAppWithFont(t *testing.T, root string, packages map[string]string) {
	t.Helper()
	fontDir := fontModuleDir(t)
	gomod := "module example.com/app\n\ngo 1.25.2\n\nrequire github.com/tinywasm/font v0.0.0\n\nreplace github.com/tinywasm/font => " + fontDir + "\n"
	writeProj(t, root, "go.mod", gomod)
	writeProj(t, root, "main.go", "package main\n\nfunc main() {}\n")
	for path, content := range packages {
		writeProj(t, root, path, content)
	}
}

const fontsRoboto = `package config

import "github.com/tinywasm/font"

func Fonts() font.Declaration {
	return font.Declare("Roboto", "config/fonts")
}
`

func TestExtract_FontsProducer(t *testing.T) {
	root := t.TempDir()
	writeAppWithFont(t, root, map[string]string{
		"config/fonts.go": fontsRoboto,
	})

	e := ssr.New(root)
	e.SetLog(t.Log)
	f := modfind.New()
	f.Seed(root, []modfind.Module{{Path: "example.com/app", Dir: root}})
	e.SetFinder(f)

	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatalf("ExtractModule: %v", err)
	}
	if assets == nil {
		t.Fatal("expected assets, got nil")
	}
	if assets.Fonts.Family() != "Roboto" {
		t.Errorf("Fonts.Family() = %q, want Roboto", assets.Fonts.Family())
	}
	if assets.Fonts.Dir() != "config/fonts" {
		t.Errorf("Fonts.Dir() = %q, want config/fonts", assets.Fonts.Dir())
	}
}

func TestExtract_NoFontsZeroValue(t *testing.T) {
	root := t.TempDir()
	writeProj(t, root, "go.mod", "module example.com/app\n\ngo 1.25.2\n")
	writeProj(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeProj(t, root, "config/css.go", `//go:build !wasm

package config

type stylesheet string

func (s stylesheet) String() string { return string(s) }

func RootCSS() stylesheet { return stylesheet(":root{--x:1}") }
`)

	e := ssr.New(root)
	e.SetLog(t.Log)
	f := modfind.New()
	f.Seed(root, []modfind.Module{{Path: "example.com/app", Dir: root}})
	e.SetFinder(f)

	assets, err := e.ExtractModule(root)
	if err != nil {
		t.Fatalf("ExtractModule: %v", err)
	}
	if assets == nil {
		t.Fatal("expected assets")
	}
	if assets.Fonts.Family() != "" {
		t.Errorf("expected zero-value Fonts, got family %q", assets.Fonts.Family())
	}
	if !strings.Contains(assets.RootCSS, "--x:1") {
		t.Errorf("existing RootCSS broken: %q", assets.RootCSS)
	}
}

func TestExtract_DuplicateFontsErrors(t *testing.T) {
	root := t.TempDir()
	writeAppWithFont(t, root, map[string]string{
		"config/fonts.go": fontsRoboto,
		"theme/fonts.go": `package theme

import "github.com/tinywasm/font"

func Fonts() font.Declaration {
	return font.Declare("Inter", "theme/fonts")
}
`,
	})

	e := ssr.New(root)
	e.SetLog(t.Log)
	f := modfind.New()
	f.Seed(root, []modfind.Module{{Path: "example.com/app", Dir: root}})
	e.SetFinder(f)

	_, err := e.ExtractModule(root)
	if err == nil {
		t.Fatal("expected error for two Fonts() declarations")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Fonts()") {
		t.Errorf("error should mention Fonts(): %v", err)
	}
	if !strings.Contains(msg, "config") || !strings.Contains(msg, "theme") {
		t.Errorf("error should name both packages, got: %v", err)
	}
}

func TestExtract_GenericFontsReceiverErrors(t *testing.T) {
	root := t.TempDir()
	writeAppWithFont(t, root, map[string]string{
		"config/fonts.go": `package config

import "github.com/tinywasm/font"

type Box[T any] struct{}

func (b *Box[T]) Fonts() font.Declaration {
	return font.Declare("Roboto", "config/fonts")
}
`,
	})

	e := ssr.New(root)
	e.SetLog(t.Log)
	f := modfind.New()
	f.Seed(root, []modfind.Module{{Path: "example.com/app", Dir: root}})
	e.SetFinder(f)

	_, err := e.ExtractModule(root)
	if err == nil {
		t.Fatal("expected error for generic Fonts receiver")
	}
	msg := err.Error()
	if !strings.Contains(msg, "generic") {
		t.Errorf("error should mention generic receiver: %v", err)
	}
	if !strings.Contains(msg, "Fonts") {
		t.Errorf("error should name Fonts: %v", err)
	}
}
