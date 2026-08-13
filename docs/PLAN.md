---
PLAN: "fix: extract only the packages the built artifact actually uses"
EXECUTOR: jules
REVIEWER: none
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.
>
> This is **stage A of a cross-repo chain**. The orchestrator lives in
> `tinywasm/app`: https://github.com/tinywasm/app/blob/main/docs/PLAN.md
> This repo is a **gate** — `assetmin` and `app` cannot start until this ships.

# Plan — extract only what the application actually uses

## The principle

```
The SSR extractor must run exactly the producers of the packages that the
artifact being served actually imports. Nothing else is walked, imported,
compiled, or merged into the stylesheet.
```

Everything below follows from that one sentence. The current behaviour —
walk every directory of every module and import anything that declares a
producer — is the cause of every defect in this plan.

## What that behaviour costs today, measured

Run from `tinywasm/layout/platformd`:

| | packages of `tinywasm/components` |
|---|---|
| in the server build graph | 7 |
| in the WASM client build graph | 8 |
| **union — what is actually used** | **9** |
| what `expandToSSRPackages` imports | **13** |

The four extra packages are dead weight in the stylesheet. One of them,
`calendarslider`, is worse than dead weight: it declares `RenderCSS()` and
imports `github.com/tinywasm/date`, which is not in layout's `go.sum` because
layout never imports calendarslider. The generated extractor imports it anyway,
so `go run` fails to build, and because that single `go run` produces **all**
modules' assets, every stylesheet in the project is lost at once:

```
ssr extract error:...calendarslider/calendarslider.go:12:2: missing go.sum entry for module providing package github.com/tinywasm/date (imported by github.com/tinywasm/components/calendarslider); to add:
	go get github.com/tinywasm/components/calendarslider@v0.5.7
```

```
GET /style.css  ->  HTTP 200, 0 bytes     # the app runs unstyled
```

The developer cannot fix this by hand. Verified on a copy of `tinywasm/layout`:
`go mod tidy` does **not** add `tinywasm/date` (nothing imports the package that
needs it), and the `go get` the error suggests adds a requirement that the next
`go mod tidy` deletes, so the error returns.

**Reachability is the fix.** A package that is not in the build graph is never
imported, so it can never break the build and never bloats the stylesheet. The
remaining stages are consequences and safety nets, not separate features.

## Reproduction (already committed)

`tests/unreachable_package_test.go` contains three failing tests. They are the
acceptance criteria — **do not modify them**. Baseline today: the whole suite
passes except those three.

---

## Stage 1 — Scope extraction to the reachable build graph

### 1.1 The scope is anchored at the started directory, not the module root

The harness is started from the root of whatever is being tested, and **that
directory is usually not the module root**. Verified:

```
$ cd components/calendarslider && go list -m -json
Path: github.com/tinywasm/components
Dir:  /home/cesar/Dev/Project/tinywasm/components    <- module root, not calendarslider
Main: true
```

`components/` has a single `go.mod`, so testing one component currently walks
the whole repository and extracts the CSS of all thirteen components.

`Extractor.rootDir` (set by `New`) already holds the started directory. Use it
as the anchor. Do **not** resolve up to the module root for scope purposes —
`findProjectRoot` stays only where a module root is genuinely required.

### 1.2 Compute the reachable set

**New file: `reach.go`** (package `ssr`).

```go
// reachSet is the set of import paths in the build graph of the started
// directory, across every build configuration the artifact is compiled for.
type reachSet map[string]bool

// GraphLister returns the transitive import paths of pattern, built for the
// given GOOS/GOARCH. Injected so tests need no toolchain.
type GraphLister func(rootDir, pattern, goos, goarch string) ([]string, error)
```

Default implementation `goListDeps`:

- Command: `go list -e -deps <pattern>`
- `cmd.Dir = rootDir`, pattern `./...`
- `-e` is **required**: under the native `GOOS` the client directory is
  WASM-only and reports `build constraints exclude all Go files`; without `-e`
  the whole listing aborts.
- Env: inherit `os.Environ()` and append `GOOS=`/`GOARCH=` when non-empty.
- Output is one import path per line; ignore empty lines.

#### The union of both build graphs is mandatory

An application is compiled twice: the server (native `GOOS`) and the WASM
client. A component imported only by the client is absent from the server graph
and vice versa. Measured from `layout/platformd`: server 7, client 8, union 9.

**Using a single graph silently drops CSS** — `fieldset` and `themetoggle` are
in the client graph only. Compute:

```go
// buildTargets are the configurations the artifact is compiled for. The
// reachable set is their UNION: a component imported only by the WASM client
// is absent from the server graph, and dropping it would lose its styles.
var buildTargets = []struct{ GOOS, GOARCH string }{
	{"", ""},        // native: the server binary
	{"js", "wasm"},  // the browser client
}
```

Union the results. If **every** target fails, log once and return an empty
`reachSet` with a flag meaning "unknown" — see 1.4.

### 1.3 Apply the filter

In `invoke.go`, `modulesToAliases` starts with:

```go
for _, m := range expandToSSRPackages(modules, scanner, assetLibraries) {
```

Filter that slice against the `reachSet`: keep a package only when its import
path is in the set. `modulesToAliases` therefore needs the `rootDir` and the
`reachSet`; thread them from `invokeSSRExtractorOnce`, which already has
`rootDir`.

Log dropped packages at most once per extraction as a single aggregate line —
one line per dropped package would reintroduce the noise this plan removes:

```go
const skippedUnreachableFmt = "ssr: %d package(s) not in the build graph were skipped"
```

### 1.4 Fail open, never closed

If the `GraphLister` cannot produce a usable set (toolchain missing, every
target failed), **do not filter**. Log once and proceed with the unfiltered
candidate list, so a broken probe degrades to today's behaviour instead of
silently emitting an empty stylesheet.

Represent this explicitly — an empty `reachSet` and an unknown `reachSet` must
not be the same value:

```go
type reachability struct {
	set   reachSet
	known bool // false => do not filter
}
```

### 1.5 Update the existing dependency-module test

`tests/extract_dependency_module_test.go` currently asserts that a local
dependency module's subpackage CSS is extracted **even though the app's
`main.go` does not import it**. That assertion encodes exactly the behaviour
this plan removes.

Edit the fixture so the app imports the package whose CSS it expects:

```go
write(filepath.Join(appDir, "main.go"),
	"package main\n\nimport _ \"example.com/layout/platformd\"\n\nfunc main() {}\n")
```

The assertion itself is unchanged and still meaningful: a dependency module's
CSS lives in its subpackages and must be collected. Do **not** weaken the
reachability filter to keep the old fixture.

**Acceptance:**

- `go test ./tests/ -run TestExtractAll_UnreachablePackageDoesNotKillExtraction` passes.
- From `layout/platformd`, the extractor imports 9 packages of
  `tinywasm/components`, not 13, and produces zero `missing go.sum entry` lines.
- From `components/calendarslider`, only calendarslider's own graph is
  extracted — not the other twelve components.

---

## Stage 2 — Safety net: packages that can never be imported

Reachability removes the cause. This stage makes the remaining cases impossible
rather than unlikely, because a package can enter the build graph and still be
un-importable.

### 2.1 A `package main` directory is never an SSR package

Every application has a client entry point (`platformd/web/client.go`), and
component libraries publish demos inside the module
(`components/calendarslider/web/`, `selectsearch/web/`, `themetoggle/web/`).
All are `package main` with `//go:build wasm`. **A `package main` cannot be
imported by anything.**

Record the package name in the scanner. In `scanner.go`, `fileFeatures` gains:

```go
type fileFeatures struct {
	mtime     time.Time
	pkgName   string          // f.Name.Name from the parsed file
	imports   map[string]bool
	producers []producerDecl
}
```

`scanFile` already holds the parsed `*ast.File`; set `pkgName: f.Name.Name`.
`packageFeatures` gains the same field, filled by `scanPackage` from the first
non-test file it reads.

In `expandToSSRPackages`, immediately after `scanner.scanPackage(path)`:

```go
// A package main cannot be imported, so it can never contribute SSR assets.
if feats.PkgName == mainPackageName {
	return nil // do not select; keep walking subdirectories
}
```

with `const mainPackageName = "main"`.

Return `nil`, **not** `filepath.SkipDir` — a `package main` directory may
contain subdirectories with real SSR packages.

#### Do NOT gate this on the module or the directory name

Both wrong alternatives were considered and rejected:

- *"skip `web/` in dependency modules"* — when the harness starts at
  `components/calendarslider`, the main module is `components` and **all three
  demos are in the main module**. A main-module exemption leaves that workflow
  unguarded.
- *"skip directories named `web` or `example`"* — depends on a naming
  convention libraries are free to ignore.

The package clause is the fact; everything else is a proxy for it.

### 2.2 Build tags are not evaluated by the scanner

`scanner.scanFile` parses with `go/parser` in mode `0`, which reads the file
regardless of its `//go:build` line. A WASM-only file declaring a producer is
therefore detected and imported into the `!wasm` extractor, where it fails to
compile.

Do **not** try to evaluate build constraints in the scanner. Stage 1 keeps such
packages out of scope and 2.1 removes the common source. Add a comment on
`scanFile` recording this so the next reader does not "fix" the parser mode.

**Acceptance:** a fixture with `<mod>/demo/client.go` containing `package main`
is never selected, in the main module and in a dependency alike.
`grep -rn '"web"' .` and `grep -rn '"example"' .` return no hits in selection
logic.

---

## Stage 3 — One extraction, one error

### 3.1 `ExtractAll` must invoke the extractor once

`ExtractAll` loops over modules calling `extractAssetsForModule`, which each
time computes the same hash key over the same module set and, on failure,
re-runs the same failing `go run` — the cache is only written on success. One
root failure becomes N compiler invocations and N identical log lines.

Restructure:

1. Resolve the shared `map[string]CollectorOutput` **once** before the loop
   (cache lookup, else `invokeSSRExtractorOnce`).
2. If that fails, return `nil, err` immediately — do **not** log per module and
   do **not** `continue`.
3. The loop then only calls `MergeResultsFor(m.path, results)` and sets
   `IsRoot` / `IsFramework`.

Extract the shared step so `ExtractModule` and `ExtractAll` use one code path:

```go
func (e *Extractor) results(rootDir string, modules []module) (map[string]CollectorOutput, error)
```

Delete the per-module error line:

```go
e.log("ssr extract error:", m.path, err)   // DELETE
```

`grep -rn "ssr extract error" .` must return zero hits in non-test code.

### 3.2 An empty result set is a failure

If the loop completes with `len(all) == 0`, return an error instead of
`(nil, nil)`:

```go
const errNoAssetsExtracted = "ssr: no assets extracted from any module; the stylesheet would be empty"
```

Returning `(nil, nil)` is what let `assetmin` report success while serving a
0-byte stylesheet.

### 3.3 A broken package in the started directory must still fail loudly

Stage 1 removes unreachable packages. A package that **is** reachable and does
not compile is the developer's own code: it must break the build with its real
compiler error, exactly as it does today. Do not add any error-swallowing path
for reachable packages.

**Acceptance:**
`go test ./tests/ -run TestExtractAll_ReportsFailureInsteadOfSilentlyReturningNothing`
passes; a genuine compile error in the project's own `css.go` produces one error
line and a non-nil error from `ExtractAll`.

---

## Stage 4 — Warn about asset libraries at most once

`noAssetLibrariesWarning` is logged at the top of both `ExtractAll` and
`ExtractModule`, i.e. on every startup and every file save. Observed ten times
in a single startup. No production caller has ever called `SetAssetLibraries` —
only this repo's tests do.

Add `warnOnce sync.Once` to `Extractor` and wrap both log sites.
`SetAssetLibraries` must assign a fresh `sync.Once` so a caller that configures
libraries later still behaves correctly on the next extraction.

Do **not** delete the warning or the `SetAssetLibraries` API — `tinywasm/app`
starts calling it in stage E of the chain.

**Acceptance:** `go test ./tests/ -run TestExtractAll_NoAssetLibrariesWarnedOnce` passes.

---

## Constraints

### This repo is backend tooling

`tinywasm/ssr` runs on the developer's machine and drives the Go toolchain. It
legitimately imports `os`, `os/exec`, `encoding/json`, `go/ast`, `go/parser`,
`sync`, `io`. The "no standard library in WASM code" rule does **not** apply
here — do **not** "fix" those imports. Use `github.com/tinywasm/fmt` for error
construction, as the existing code does.

### No hardcoded strings

Every repeated string is a named constant. The plan names the required ones:
`skippedUnreachableFmt`, `mainPackageName`, `errNoAssetsExtracted`,
`buildTargets`.

### Behaviour that must NOT change

- The producer-panic recovery in the generated `main.go` template
  (`failures []failure`) stays as is.
- `MergeResultsFor`, the `@layer` conflict check and the `Fonts()` uniqueness
  check are unchanged.
- Producers are still detected by **name and receiver type only**; correctness
  of the call stays delegated to the Go compiler.

### Do not

- Do not call `go get` or `go mod tidy` from this repo. Repairing a consumer's
  `go.mod` is not ssr's job, and for the calendarslider case it does not work.
- Do not add `gopush` or `codejob` calls anywhere.
- Do not keep a "walk everything" fallback alongside the reachability filter.
  The only fallback is the explicit `known == false` case in 1.4.

---

## Stages

| # | Scope | Files | Acceptance |
|---|---|---|---|
| 1 | Scope extraction to the reachable build graph, anchored at the started directory, union of native + WASM | `reach.go` (new), `invoke.go`, `ssr.go`, `tests/extract_dependency_module_test.go` | 9 of 13 component packages from `layout/platformd`; `TestExtractAll_UnreachablePackageDoesNotKillExtraction` passes |
| 2 | `package main` is never an SSR package; scanner records the package clause | `scanner.go`, `invoke.go` | demo dirs never selected in any module |
| 3 | One extraction, one error; empty = failure | `ssr.go`, `extract.go` | `grep -rn "ssr extract error" .` empty in non-test code |
| 4 | Warn once about asset libraries | `ssr.go` | `TestExtractAll_NoAssetLibrariesWarnedOnce` passes |

Final gate:

```
go test ./...
```

fully green, including the pre-existing tests in `tests/`.
