package sitec

import (
	"fmt"
	"strings"
)

// skippedUnreachableSummaryFmt names every skipped package in one line. Only
// packages that already declare assets reach this point, so "skipped" means
// their styles and icons will not ship — the silent failure this diagnostic
// exists to catch. It used to be one log line per package
// (skippedUnreachableFmt): with Extractor's cache cold on every process
// restart, a project with a dozen unreachable sibling packages flooded the
// TUI with an identical-shaped line per package, on every boot. One line
// keeps the full information (the complete list) without the per-package
// noise. It only fires when verbose logging is on and the reachability data
// is complete (see reachabilityPartialFmt) — a partial computation must not
// be reported as a confident list of "these won't ship".
const skippedUnreachableSummaryFmt = "sitec: %d package(s) skipped — they declare assets but are not in the ./... build graph; " +
	"their styles and icons will NOT ship: %s"

// reachabilityProbeFailedFmt reports a single build-target probe failure
// (e.g. `go list -deps ./...` for GOOS=js GOARCH=wasm). Logged only when
// verbose, since a probe failure is common on a cold module cache and
// resolves itself on the next scan — see reachabilityPartialFmt for the
// user-facing consequence.
const reachabilityProbeFailedFmt = "sitec: reachability probe failed for GOOS=%q GOARCH=%q: %v"

// reachSet is the set of import paths in the build graph of the start
// directory, across every configuration the artifact is compiled for.
type reachSet map[string]bool

// GraphLister returns the transitive import paths of pattern for the given
// GOOS/GOARCH. Injected so tests don't need a real toolchain.
type GraphLister func(rootDir, pattern, goos, goarch string) ([]string, error)

// buildTargets are the configurations the artifact is compiled for. The
// reachable set is their UNION: a component that only imports the WASM
// client never shows up in the server's graph, and dropping it would lose
// its styles.
var buildTargets = []struct{ GOOS, GOARCH string }{
	{"", ""},       // native: the server binary
	{"js", "wasm"}, // the browser client
}

type reachability struct {
	set   reachSet
	known bool // false => don't filter: no target succeeded, data unusable
	// partial is true when at least one build target's probe failed (e.g. a
	// cold module cache on process startup). The union is then INCOMPLETE —
	// treating it as ground truth risks excluding a package that is, in
	// fact, reachable, silently dropping its styles. Callers must treat
	// partial like !known for filtering purposes: better to keep an
	// actually-unreachable package around for one extra scan than to drop a
	// reachable one.
	partial bool
}

// goListDeps is the default GraphLister implementation.
func goListDeps(rootDir, pattern, goos, goarch string) ([]string, error) {
	tc := NewExecToolchain()
	env := []string{}
	if goos != "" {
		env = append(env, "GOOS="+goos)
	}
	if goarch != "" {
		env = append(env, "GOARCH="+goarch)
	}

	var data []byte
	var err error
	if len(env) > 0 {
		data, err = tc.ListEnv(rootDir, env, "-e", "-deps", pattern)
	} else {
		data, err = tc.List(rootDir, "-e", "-deps", pattern)
	}
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, nil
}

// computeReachability computes the union of reachable packages across every
// build target. log receives a diagnostic when a target's probe fails; pass
// nil to stay silent (non-verbose callers).
func computeReachability(rootDir string, lister GraphLister, log func(...any)) reachability {
	if lister == nil {
		lister = goListDeps
	}

	set := make(reachSet)
	anySuccess := false
	anyFailure := false

	for _, target := range buildTargets {
		deps, err := lister(rootDir, "./...", target.GOOS, target.GOARCH)
		if err != nil {
			anyFailure = true
			if log != nil {
				log(fmt.Sprintf(reachabilityProbeFailedFmt, target.GOOS, target.GOARCH, err))
			}
			continue
		}
		anySuccess = true
		for _, dep := range deps {
			set[dep] = true
		}
	}

	if !anySuccess {
		return reachability{set: nil, known: false}
	}
	return reachability{set: set, known: true, partial: anyFailure}
}
