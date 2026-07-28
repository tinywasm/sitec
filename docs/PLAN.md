# PLAN — total detection, deduplicated merge

Execution document. Steps, reference code, test strategy. **Ephemeral**: not
indexed by `README.md`, and no permanent document links here.

Everything this plan needs is specified elsewhere. Consult those documents only
when a step is ambiguous — do not re-derive their content here:

| If you need… | Read |
|---|---|
| exact detection rules, merge semantics, error messages | [SPECS.md](SPECS.md) |
| why a decision was made, or what was already rejected | [DESIGN.md](DESIGN.md) |
| the structure and guarantees being preserved | [ARCHITECTURE.md](ARCHITECTURE.md) |
| the pipeline, and where assets are lost today | [diagrams/EXTRACTION.md](diagrams/EXTRACTION.md) |

---

## Development Rules

- **Documentation first.** The documents above are written to the target. Code
  follows them; if code and SPECS disagree, decide which is wrong and fix both in
  the same commit.
- **Never fail silently.** An asset that should have been collected and was not is
  a defect. Make the extractor find it, or fail the build naming the package.
  Never skip quietly.
- **Detection matches the method name only** — never signature, return type, or
  the package a returned type comes from. This is what lets a producer change its
  return type without touching the extractor. Hard constraint on every step below.
- **Zero-value instantiation.** Producers run on `&T{}`.
- **Deterministic output.** Merged assets must not shuffle between runs.

---

## 1. Goal

A component author writes only the component. A package that declares a producer
is collected because it declares one — no registry, no init, no manifest, and no
filename to remember.

---

## 2. Defects this plan closes

Each was reproduced by executing the detection code at `ebf2e45`. Keep the
reproducers; they become the tests in §5.

**E-1 — a second producer type in the same package is dropped.**
`detectReceiverType` uses `FindSubmatch`, which returns only the first match. Run
against a package declaring `RenderCSS` on `Alpha` and `Beta`:

```
detectReceiverType = "Alpha"
receivers actually present: 2   (Alpha, Beta)
```

The generated program instantiates `&ui.Alpha{}` only. `Beta` ships unstyled with
a green build.

**E-2 — a producer outside the four known filenames is invisible.** Detection
reads only `css.go`, `js.go`, `svg.go`, `html.go`. A `RenderCSS` in `widget.go` is
never seen — and because `hasCSSSource()` is also false, the "declares no
producer" guard never fires either. The loud error only protects the author who
already got the filename right.

**E-3 — the regexes require a single-line signature.** A gofmt-legal receiver
split across lines, or a generic receiver, yields no assets and no error.

**E-4 — the layer statement repeats once per component.** Sheets are merged by
`+=`, so the cascade order of the whole application depends on which sheet sorts
first. Two components already produce two statements.

**E-5 — identical declaration blocks recur across components.** No single sheet
can know how many others exist, so the redundancy is only removable here.

**E-6 — the zero-value contract is undocumented.** Producers run on `&T{}`;
nothing tells authors so and nothing tests it.

**E-7 — a panicking producer takes down the whole extraction opaquely.**
`invokeSSRExtractorOnce` runs one generated program over every package at once,
so a panic in any producer fails the run with a stack pointing at generated code
the author never wrote. `tinywasm/widget` is about to make this reachable on
purpose: an invalid sheet panics, by design, so a misspelt part name in one
component will abort the extraction for all of them with no indication of which
package caused it.

---

## 3. Coordination

`tinywasm/widget` is landing a breaking release that removes unreachable
`.fl-*` / `.exc-*` selectors from every sheet. **Measure steps 4 and 5 after that
lands**, so the dedupe work is sized against real output rather than today's.

Steps 1–3 are independent of it and can start now.

---

## 4. Implementation order

**Step 1 — `go/ast` detection.** Replaces the ten regexes in `invoke.go` and the
`ssrSourceFiles` list in `extract.go`. Reuse the mtime cache in `scanner.go`.
Closes **E-2**, **E-3**. SPECS §1.

Walk every non-test file; for each `*ast.FuncDecl` whose `Name.Name` is a producer
name, record the name and — if there is a receiver — its type identifier. Match on
the name only; never inspect `Type.Results`.

**Step 2 — N producer types per package.** `moduleAlias.ReceiverType string`
becomes `ReceiverTypes []string`, sorted; the generated `main.go` template loops
over it and concatenates. Closes **E-1**. SPECS §1.2, §1.3.

```go
{{range .ReceiverTypes}}
{
    inst := &{{$.Alias}}.{{.}}{}
    {{if $.HasRoot}}s.Root += inst.RootCSS().String(){{end}}
    {{if $.HasRender}}s.Render += inst.RenderCSS().String(){{end}}
}
{{end}}
```

Note `s.Root +=`, not `s.Root =`: with several types the assignment form silently
kept the last one.

**Step 3 — widen the missing-producer error.** Key it on the package's imports
rather than on `css.go` existing, now that step 1 has removed the filename
dependency. Closes the silent half of **E-2**. SPECS §3.

The import list is configuration, not a constant — `widget/style` today, more
later.

**Step 4 — hoist the layer statement.** In `MergeResultsFor`. Collect every
`@layer …;`, error on conflict, emit one first, strip the rest. Closes **E-4**.
SPECS §4.1.

**Step 5 — merge identical blocks.** Same layer, byte-identical declarations, no
intervening overlapping selector; the merged rule takes the first occurrence's
position. Closes **E-5**. SPECS §4.2.

This is an optimisation, not a correctness fix. **If the third condition cannot be
established cheaply, ship steps 1–4 and drop this one** — see
[DESIGN.md §5](DESIGN.md#5-why-identical-blocks-are-merged-and-where-the-merge-stops).

**Step 6 — recover per producer.** The generated program wraps each producer call
in its own `recover()`, records the package path, the receiver type and the
panic value, and reports them together at the end rather than aborting on the
first. Closes **E-7**.

```go
func() {
    defer func() {
        if r := recover(); r != nil {
            failures = append(failures, failure{Pkg: "{{.Path}}", Type: "{{.}}", Err: fmt.Sprint(r)})
        }
    }()
    s.Render += inst.RenderCSS().String()
}()
```

The run still fails — a producer that panics is a defect, and this module does
not skip quietly. It fails *naming the package and type*, which is the whole
difference between a thirty-second fix and an afternoon.

**Step 7 — documentation.** Fold the author contract into `ARCHITECTURE.md §3`
(already written), and confirm `README.md` indexes every permanent document.
Closes **E-6**.

**Step 8 — remove the STATUS markers** from `ARCHITECTURE.md` and `SPECS.md`, and
delete `SPECS.md §6` once published behaviour matches the target. They exist
because those documents were written ahead of the implementation; removing them is
the last act of this plan.

---

## 5. Test strategy

Every test names the defect it closes.

| Test | Asserts | Closes |
|---|---|---|
| `TestTwoProducersOnePackage` | a package declaring `RenderCSS` on `Alpha` and `Beta` emits both stylesheets, in type-name order | E-1 |
| `TestProducerOutsideCssGo` | a `RenderCSS` in `masterdetail.go`, with no `css.go`, is collected | E-2 |
| `TestNoProducerIsAnError` | a package importing `widget/style` and declaring none fails the build, naming the package | E-2 |
| `TestProducerMultilineSignature` | a receiver split across lines is detected | E-3 |
| `TestProducerGenericReceiver` | `*Table[T]` is detected, and either collected or reported — never skipped silently | E-3 |
| `TestSingleLayerStatement` | merged output has exactly one `@layer …;`, before any rule | E-4 |
| `TestConflictingLayerOrderErrors` | two packages with different layer orders is an error, not last-one-wins | E-4 |
| `TestIdenticalBlocksMerged` | two components using the same primitive emit one rule with both selectors | E-5 |
| `TestMergeStopsAtOverlap` | **counter-fixture**: an intervening rule targeting an overlapping selector prevents the merge | E-5 |
| `TestPanicNamesProducer` | a producer that panics fails the run with a message naming its package and receiver type, not a generated-code stack | E-7 |
| `TestZeroValueProducer` | a producer whose output would differ if a field were read still emits the zero-value form | E-6 |
| existing `deterministic_order_test.go` | byte-identical merge across runs — keep, extend to cover steps 2 and 5 | — |

`TestMergeStopsAtOverlap` is not optional. A merge optimisation without a test
proving where it stops is how cascade bugs get shipped.

---

## 6. Acceptance criterion

Build a fixture app with four components:

1. two in one package,
2. one whose producer lives in `masterdetail.go` rather than `css.go`,
3. one importing `widget/style` and declaring no producer.

Today: components 2, 3 and 4 all ship unstyled, and the build is green three times
over.

After this plan: 1–3 are collected, and 4 fails the build naming the package.
