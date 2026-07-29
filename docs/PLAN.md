---
PLAN: "fix: surface the disabled producer guard and reject generic receivers by name"
TAG: v0.0.27
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 15324244369777097270
---

# PLAN — the guarantees that shipped disabled

Execution document. Steps, reference code, test strategy. **Ephemeral**: not
indexed by `README.md`, and no permanent document links here.

The previous plan (total detection, deduplicated merge) is **executed and
verified** — its content lives in git at `a01fe2e`. This plan closes what that
one left open: two guarantees that pass their tests but do not hold in the real
pipeline, plus the documentation debt it left behind.

| If you need… | Read |
|---|---|
| exact detection rules, merge semantics, error messages | [SPECS.md](SPECS.md) |
| why a decision was made, or what was already rejected | [DESIGN.md](DESIGN.md) |
| the structure and guarantees being preserved | [ARCHITECTURE.md](ARCHITECTURE.md) |
| the pipeline | [diagrams/EXTRACTION.md](diagrams/EXTRACTION.md) |

---

## Development Rules

- **Documentation first.** If code and SPECS disagree, decide which is wrong and
  fix both in the same commit.
- **Never fail silently.** An asset that should have been collected and was not is
  a defect. This plan exists because the rule was applied to the code and not to
  the *configuration* of the code.
- **Detection matches the method name only** — never signature, return type, or
  the package a returned type comes from.
- **Zero-value instantiation.** Producers run on `&T{}`.
- **Deterministic output.** Merged assets must not shuffle between runs.
- **Stay decoupled from `widget`.** The extractor must not name a styling library
  in its own source; that decoupling (`ebf2e45`) is deliberate and stays.

---

## 1. What was verified as done

Confirmed against the code and a green `gotest` (`vet ✅, race ✅, tests ✅`,
coverage 76.6%):

| Step | Evidence |
|---|---|
| 1 — `go/ast` detection | `scanner.go`: walks every non-test `.go`, matches on `fn.Name.Name` against a producer-name set, never inspects `Type.Results`; generic receivers unwrap through `IndexExpr`/`IndexListExpr` |
| 2 — N producer types per package | `modulesToAliases` groups by receiver and sorts by name; the template loops and concatenates with `+=` |
| 3 — missing-producer error keyed on imports | `modulesToAliases`, driven by `AssetLibraries`, default empty — **but see §2, F-1** |
| 4 — cascade-layer conflict | `MergeResultsFor` + `extractLayers`; the message names both packages |
| 5 — identical-block merge | deliberately deleted; recorded in SPECS §4.2 |
| 6 — recover per producer | generated `main.go` wraps each receiver in its own `recover()`, accumulates `failures`, reports all and exits 1 |
| 7 / 8 — docs | STATUS markers gone, SPECS §6 deleted, `README.md` indexes every permanent document |

The eight tests named in the old plan all exist in `tests/total_detection_test.go`.

**Coordination with `tinywasm/widget` is closed.** The old §3 held steps 4 and 5
until widget's `.fl-*`/`.exc-*` removal landed, so the dedupe work could be sized
against real output. Step 5 no longer exists, and step 4 is a conflict check whose
cost does not depend on sheet size. Nothing here waits on widget any more. The one
requirement widget delegated — a panicking producer must name its package — is
step 6, done.

---

## 2. Defects this plan closes

**F-1 — the missing-producer guard is off in production.** `AssetLibraries`
defaults to `[]string{}` and nothing ever sets it: `tinywasm/app` builds the
extractor at `app/section-build.go:256` and wires `SetLog` and `SetFinder`, but
never `SetAssetLibraries`. `TestNoProducerIsAnError` passes only because the test
injects the list itself.

So the acceptance criterion of the previous plan — *"component 4 fails the build
naming the package"* — **is not met in a real application**. A component that
imports a styling library and forgets its producer still ships unstyled with a
green build, which is the exact defect (E-2) the previous plan claimed to close.

A guard that is silently disabled is worse than an absent one: the test suite
reports it as present.

**F-2 — a generic receiver fails with the error the module exists to remove.**
Detection unwraps `*Table[T]` to `Table`, then the generated program emits
`&pkg.Table{}` and the Go compiler rejects it. The author gets a compile error
pointing at generated code they never wrote — the same failure shape as E-7,
which step 6 fixed for panics and not for this. `TestProducerGenericReceiver`
asserts only that *some* error occurs, so it passes on the bad message.

**F-3 — SPECS numbering has a hole.** Step 8 deleted §6 without renumbering: the
document goes §5 → §7. Cosmetic, but SPECS is the authority document and a
reference to "§7" now means different sections depending on when the reader
looked.

---

## 3. Implementation order

**Step 1 — make the disabled guard visible (`ssr.go`).** Keep the default empty:
the decoupling is right and hardcoding `widget/style` here would undo `ebf2e45`.
What is wrong is that "off" is indistinguishable from "on and finding nothing".
Log it once per extraction run, through the logger the application already wires:

```go
// in the extraction entry point, once per run
if len(e.AssetLibraries) == 0 {
    e.log("ssr: no asset libraries configured; packages that import a styling library",
        "and declare no producer will NOT fail the build (see SetAssetLibraries)")
}
```

This module's rule is "never fail silently". A guard that is silently off breaks
that rule at the configuration layer instead of the code layer, which is why the
test suite could not see it.

**Step 2 — inject the list from `tinywasm/app`.** ⚠️ **Not part of this repo's
work — do not attempt it here.** It is recorded so the reason for step 1 is
legible, and it is dispatched separately in `tinywasm/app`. One line beside the
two that already exist at `app/section-build.go:256-258`:

```go
ssrExtractor := ssr.New(h.RootDir)
ssrExtractor.SetLog(h.Watcher.Logger)
ssrExtractor.SetFinder(h.Finder)
ssrExtractor.SetAssetLibraries([]string{"github.com/tinywasm/widget/style"})
```

`widget/style` is the ecosystem's asset-producing library today; the parameter
stays a list so an application can add its own without ssr learning any name.
Whether the list becomes app configuration later is a separate decision — what
cannot wait is that it is non-empty in the shipped pipeline.

**Step 3 — reject generic receivers by name, at scan time (`scanner.go`).**
`getReceiverTypeName` already knows it unwrapped an `IndexExpr`; carry that fact
into `producerDecl` and fail in `modulesToAliases` with the module's own error
shape:

```
ssr: package <path> declares producer <Name>() on generic type <Type>[…];
generic receivers cannot be instantiated as a zero value — use a concrete type
```

The author gets the package, the type and the reason. Generic producers stay
unsupported — the zero-value contract has no way to choose a type argument — but
they stop being reported by the Go compiler in the wrong place.

**Step 4 — SPECS (`docs/SPECS.md`).** Renumber §7 → §6, document the generic
receiver in §3's error table, and state in §3 that the guard is inert while
`AssetLibraries` is empty and that the emptiness is logged.

---

## 4. Test strategy

Every test names the defect it closes.

| Test | Asserts | Closes |
|---|---|---|
| `TestEmptyAssetLibrariesIsLogged` | an extraction with no configured libraries emits the warning through `SetLog`; with a list configured it does not | F-1 |
| `TestProducerGenericReceiver` (rewrite) | the error names the package **and** the generic type, and does not come from the Go compiler — assert on the message, not on `err != nil` | F-2 |
| existing `TestNoProducerIsAnError` | unchanged; it already covers the configured path | — |
| existing `deterministic_order_test.go` | unchanged | — |

`gotest` must stay green, including the race detector: the warning is emitted
under the extractor's mutex-guarded entry point, and `AssetLibraries` is written
by `SetAssetLibraries` from another goroutine in the app.

---

## 5. Acceptance criterion

Within this repo:

1. An extraction run with no configured asset libraries states so through the
   logger the application wired; with a list configured it stays quiet. A reader
   of the build output can tell "guard off" from "guard on and finding nothing"
   — which is the whole defect F-1 leaves in the pipeline.
2. A producer declared on `Table[T any]` fails with the message from step 3 —
   package, type and reason — and never with a compiler error in generated code.
3. `gotest` green, including the race detector.

Cross-repo, tracked in `tinywasm/app` and **not** closed by this plan: an app
whose `components/` package imports `widget/style` and declares no producer must
fail the build naming the package. Today it builds green and ships unstyled.
