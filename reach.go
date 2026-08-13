package ssr

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
)

// reachSet is the set of import paths in the build graph of the started
// directory, across every build configuration the artifact is compiled for.
type reachSet map[string]bool

// GraphLister returns the transitive import paths of pattern, built for the
// given GOOS/GOARCH. Injected so tests need no toolchain.
type GraphLister func(rootDir, pattern, goos, goarch string) ([]string, error)

type reachability struct {
	set   reachSet
	known bool // false => do not filter
}

// buildTargets are the configurations the artifact is compiled for. The
// reachable set is their UNION: a component imported only by the WASM client
// is absent from the server graph, and dropping it would lose its styles.
var buildTargets = []struct{ GOOS, GOARCH string }{
	{"", ""},       // native: the server binary
	{"js", "wasm"}, // the browser client
}

// goListDeps is the default GraphLister implementation.
func goListDeps(rootDir, pattern, goos, goarch string) ([]string, error) {
	cmd := exec.Command("go", "list", "-e", "-deps", pattern)
	cmd.Dir = rootDir

	// Inherit os.Environ() and append GOOS/GOARCH if non-empty
	env := os.Environ()
	if goos != "" {
		env = append(env, "GOOS="+goos)
	}
	if goarch != "" {
		env = append(env, "GOARCH="+goarch)
	}
	cmd.Env = env

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	lines := strings.Split(out.String(), "\n")
	var deps []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			deps = append(deps, trimmed)
		}
	}
	return deps, nil
}

// computeReachability computes the union of dependencies across all build targets.
func computeReachability(rootDir string, lister GraphLister, log func(...any)) reachability {
	if lister == nil {
		lister = goListDeps
	}

	union := make(reachSet)
	anySuccess := false

	for _, target := range buildTargets {
		deps, err := lister(rootDir, "./...", target.GOOS, target.GOARCH)
		if err != nil {
			continue
		}
		anySuccess = true
		for _, dep := range deps {
			union[dep] = true
		}
	}

	if !anySuccess {
		if log != nil {
			log("ssr: reachability graph could not be resolved, proceeding without filtering")
		}
		return reachability{
			set:   nil,
			known: false,
		}
	}

	return reachability{
		set:   union,
		known: true,
	}
}
