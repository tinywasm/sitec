# PLAN — Migrate ssr module discovery to `tinywasm/modfind`

> `ssr` currently runs its own `go list -m -json all` inside `discoverModules`
> ([extract.go:100](../extract.go#L100)) and maps it to an internal `module{path, dir}`. That exact
> `go list` block is **also** copy-pasted in `image/min` and `imagemin`. This plan replaces ssr's copy
> with a call to the shared **`tinywasm/modfind`** finder, so `go list -m -json all` runs **once** per
> dev session and is cached/shared across all asset+schema tooling.
>
> **Self-contained, single-module plan** (`tinywasm/ssr`). Prerequisite: `tinywasm/modfind` published.
> Its contract (described here so this plan needs no external file): `modfind.New() *Finder`;
> `(*Finder).Discover(rootDir) ([]Module, error)` runs `go list -m -json all` once and caches;
> `(*Finder).Dirs(rootDir) ([]string, error)`; `(*Finder).Seed(rootDir, []Module)` pre-loads the
> cache for tests; `Module{Path, Dir, Version string; IsMain, IsReplace, Indirect bool}` with
> `Writable() bool`.
>
> **This is a clean breaking change.** The duplicated `go list` AND the old `listModulesFn` injection
> seam are **deleted** — no deprecated fallback is kept. Output assets are unchanged.

---

## 1. Development Rules (constraints copied for execution context)

- **REUSE `modfind` — do NOT reimplement discovery.** The `go list -m -json all` logic already exists,
  consolidated and tested, in the **published** `github.com/tinywasm/modfind`. This task is a
  **deletion + wiring** correction, **not** a from-scratch implementation. Do not write any `go list`,
  `os/exec`, or module-walk code in ssr — import modfind and call `Discover`/`Dirs`. The only copy of
  this algorithm in the ecosystem must be modfind's.
- **Same assets, single discovery source.** `ExtractModule`/`ExtractAll` must return the same assets
  as today. Only the *source* of the module list changes (local `go list` → `modfind`).
- **Delete the old seam — no deprecated code.** Remove `listModulesFn` + `SetListModulesFn`
  ([ssr.go:37](../ssr.go#L37)) entirely. Tests no longer inject a `func`; they inject a
  `*modfind.Finder` seeded via `Seed(rootDir, mods)`. One injection path, not two.
- **`ssr` stays tool-side.** It already shells out; `modfind` is `//go:build !wasm` too. No new WASM
  exposure.
- **Minimal dep.** `modfind` pulls only stdlib + `tinywasm/fmt` (which ssr already imports). No heavy
  transitive deps enter ssr's graph.
- **Documentation first.**

---

## 2. Problem

`discoverModules` ([extract.go:100-122](../extract.go#L100)) duplicates the canonical `go list -m
-json all` loop. `ssr`, `image/min`, and `imagemin` each run it separately at startup → the costly
`go list` executes 3× for the same project, and none shares a cache. The dev loop is meant to be
light and fast; this is wasted work.

---

## 3. Decision

Delete ssr's inline `go list` and the `listModulesFn` seam; source modules from a `*modfind.Finder`:

```go
// ssr.go — single discovery path
func (e *Extractor) discoverModules(rootDir string) ([]module, error) {
    if e.finder == nil {
        e.finder = modfind.New() // lazy: standalone use still works
    }
    found, err := e.finder.Discover(rootDir)
    if err != nil { return nil, err }
    var mods []module
    for _, m := range found {
        mods = append(mods, module{path: m.Path, dir: m.Dir})
    }
    return mods, nil
}

// app injects ONE shared finder across ssr + image + ormc:
func (e *Extractor) SetFinder(f *modfind.Finder) { e.finder = f }
```

- Remove the `listModulesFn` field, `SetListModulesFn`, and the package-level `discoverModules`
  (the raw `go list` runner in [extract.go](../extract.go)) — all deleted, no fallback.
- `Extractor` gains a `finder *modfind.Finder` field + `SetFinder`. When `app` injects the shared
  finder, ssr/image/ormc share ONE `go list`; standalone ssr lazily makes its own.
- **Tests** that previously called `SetListModulesFn(fakeFn)` now do
  `f := modfind.New(); f.Seed(root, []modfind.Module{{Path:…, Dir:…}}); e.SetFinder(f)`.

---

## 4. Implementation Steps

### Step 1 — Bump modfind
`go get github.com/tinywasm/modfind@vX`.

### Step 2 — Finder field + injector; remove old seam
[ssr.go](../ssr.go): add `finder *modfind.Finder` to `Extractor` and `SetFinder`. **Delete** the
`listModulesFn` field and `SetListModulesFn`.

### Step 3 — Replace discovery
[ssr.go](../ssr.go): rewrite the `discoverModules` **method** to use `e.finder.Discover` with lazy
init (§3). [extract.go](../extract.go): **delete** the package-level `discoverModules` `go list`
runner (lines ~100-122) and its now-unused imports (`bytes`, `encoding/json`, `os/exec` if unused
elsewhere).

### Step 4 — Migrate tests off `listModulesFn`
Update every test that called `SetListModulesFn` to seed a finder instead (§3, last bullet). This is
the breaking part inside ssr's own suite.

### Step 5 — Documentation
[README.md](../README.md): note module discovery is delegated to `modfind` (shared, cached). Link
`modfind`.

---

## 5. Edge Cases

- **Seeded finder (tests)** → `Discover` returns the seeded modules without running `go list`.
- **`modfind` returns a replace module** → ssr treats `Dir` the same as any module dir (assets
  extracted in place). No special handling needed in ssr.
- **`go list` fails inside modfind** → error bubbles up; `ExtractModule` already falls back to a
  single synthesized module ([ssr.go:46](../ssr.go#L46)).
- **Shared finder injected by app** → first ssr/image/ormc call triggers the single `go list`; the
  rest hit modfind's cache.

---

## 6. Test Strategy

`gotest`. Existing SSR extraction tests (asset golden output) are the regression guard — they must
pass byte-for-byte. Add:

| # | Case | Assert |
|---|------|--------|
| SR1 | seeded finder injected | modfind `go list` not invoked; seeded modules used |
| SR2 | no injected finder | lazy modfind finder discovers the real project modules |
| SR3 | injected shared finder | ssr uses it; module list matches `finder.Discover` |
| SR4 | golden asset output | unchanged vs pre-refactor (regression) |

---

## 7. Out of Scope

- `modfind`'s implementation — its own plan.
- Schema sync / `model_orm.go` — ormc's concern; ssr only handles assets.
- `image/min` + `imagemin` migrations — their own plans (same pattern).
