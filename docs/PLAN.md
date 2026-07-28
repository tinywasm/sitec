# PLAN — make extraction total, deduplicated, and impossible to fail silently

## Development Rules

Copied from `docs/DOCUMENTATION.md`, plus the constraints this module already
operates under.

- **Documentation first.** Update the docs before writing code.
- **Never fail silently.** An asset that should have been collected and was not is
  a defect, not a warning. The module already takes this position in
  `invoke.go` — *"a misnamed builder is otherwise emitted nowhere and fails
  silently — the component renders unstyled and the build stays green"* — and this
  plan extends it to the cases that still slip through.
- **Feature detection matches on the METHOD NAME only**, never on return type or
  the package that type comes from. That is what let `IconSvg()` change its return
  type without touching a pattern, and it is why the generated `main.go` can keep
  `Icons any`. Preserve this property.
- **Zero-value instantiation.** Providers are called on `&T{}`. This is a contract
  with the component author, and it must be documented rather than assumed.
- **Deterministic output.** Merged CSS must not shuffle between runs; the existing
  sorted merge in `MergeResultsFor` is a guarantee, not an implementation detail.
- **Ephemeral document.** Not indexed by `README.md`. Rationale belongs in
  `DESIGN.md`, contracts in `ARCHITECTURE.md`.

---

## 1. Goal

`ssr` is the delivery layer of the three-library split:

| Library | Owns | Never does |
|---|---|---|
| `tinywasm/css` | **Values** — what a colour, space, duration or z-level *is*; light/dark switching; contrast guarantees | Know anything about components |
| `tinywasm/widget` | **Decisions** — which token applies to which part in which state | Invent a value |
| `tinywasm/ssr` | **Delivery** — collect the sheets actually used, order them, deduplicate them | Know what a widget is |

The design principle is shared with `tinywasm/widget`:

> **Less is more.** The author of a component should write the smallest possible
> amount of ceremony to be collected, and every rule they must remember in order
> to be collected correctly is a rule that will eventually be forgotten. Prefer
> deleting a convention over documenting it.

Concretely, a component author's obligation should be exactly this and nothing
more:

```go
// css.go
package masterdetail

func (m *MasterDetail) RenderCSS() *css.Stylesheet {
    return style.Of(m.WidgetName()).
        Root(style.Grid(style.TrackSm, style.SpaceSm), style.On(style.Page)).
        Part("item", style.Row(style.SpaceXs), style.Interactive(style.Muted)).
        Stylesheet()
}
```

One file, one method, no registration, no init, no separate `Style()` step.

---

## 2. Findings

All verified against `ebf2e45`, by reading and by executing the detection code —
not assumed.

### 2.1 A second component in the same package is silently dropped

`detectReceiverType` uses `FindSubmatch`, which returns only the **first** match:

```go
m := re.FindSubmatch(content)
if len(m) > 1 { … detected = found }
```

Executed against a package declaring two providers:

```go
func (a *Alpha) RenderCSS() *css.Stylesheet { … }
func (b *Beta)  RenderCSS() *css.Stylesheet { … }
```
```
detectReceiverType = "Alpha"
receivers actually present: 2   (Alpha, Beta)
```

The generated `main.go` instantiates `&ui.Alpha{}` only. **`Beta` ships
unstyled, with a green build** — precisely the failure mode the module's own
comment says it exists to prevent.

There is an implicit "one component per package" rule here that is nowhere
documented and nowhere enforced.

### 2.2 A provider outside the four known filenames is invisible

```go
var ssrSourceFiles = []string{"css.go", "js.go", "svg.go", "html.go"}
```

Detection reads **only** these four files. A `RenderCSS()` written in
`widget.go`, `styles.go` or `masterdetail.go` is never seen. And because
`hasCSSSource()` is also false in that case, the guard that would have raised the
"declares no provider" error never fires either: the package is simply skipped.

So the loud error only protects the author who already got the filename right.
The author who did not gets silence — the same silence that once shipped an empty
`style.css`.

### 2.3 Widget sheets repeat their preamble once per component

`MergeResultsFor` concatenates: `merged.Render += out.Render`. Each
`widget/style` sheet begins with its own layer statement and its own primitives
block. Two trivial widgets already produce:

```
@layer statement repeats: 2
.fl-stack occurrences:    4
```

`tinywasm/widget` is fixing its side (it emits unreachable `.fl-*` selectors that
no markup can reference). But the layer statement will still repeat once per
component, and identical declaration blocks will still recur across components,
because no single sheet can know how many others exist. **Deduplication is
structurally `ssr`'s job**, not the widget library's.

### 2.4 The zero-value contract is undocumented

The generated `main.go` does `inst := &m_x.T{}` and calls the provider. If a
component's `RenderCSS()` reads a field, it silently emits CSS built from zero
values. Nothing states this constraint to component authors, and nothing tests it.

### 2.5 Regex detection is brittle in ways that fail quietly

`^func \(\w+ \*?(\w+)\) RenderCSS\(\)` requires the signature to be on one line
with single spaces. A gofmt-legal variation — a receiver split across lines, a
generic receiver `func (w *Table[T]) RenderCSS()` — does not match. The result is
never an error; it is an absent stylesheet.

`go/parser` is already a dependency (`scanner.go` uses it for imports), so the
brittleness buys nothing.

---

## 3. Design decisions

**D1 — Detect providers with `go/ast`, over every non-test `.go` file in the
package.**
Deletes the four-filename convention (2.2) and the single-line-signature
fragility (2.5) in one move. Keeps the house rule intact: match on **method name
only**, ignore signature and return type. `scanner.go` already caches parses by
mtime; reuse that cache.

The `css.go` convention survives as a style preference, not a correctness trap —
which is the "delete a convention rather than document it" principle applied.

**D2 — Support N providers per package.**
Collect every receiver type declaring a provider, instantiate each, and
concatenate their output in sorted type-name order so emission stays
deterministic. Fixes 2.1.

**D3 — Keep the loud error, and widen what it covers.**
Today: "has `css.go` but declares no provider". After D1 the check becomes: a
package that imports `tinywasm/widget/style` but declares no `RenderCSS()` is an
error. That reaches the author who never created a `css.go` at all — the case
that is silent today.

**D4 — Hoist and deduplicate the cascade-layer statement.**
The merged output carries exactly one `@layer …;` statement, first, before any
rule. Repeats are stripped. If two packages declare **different** layer orders,
that is an error, not a silent last-one-wins — the cascade order of the whole app
depends on it.

**D5 — Merge byte-identical declaration blocks within the same layer.**
Two components that both use `Stack` emit the same three declarations under
different selectors. Merging them into one rule with a combined selector list is
a real size win at app scale.

Safety condition, which the test must encode: merge only rules **inside the same
`@layer`**, only when the declaration blocks are byte-identical, and keep the
position of the **first** occurrence. Rules in different layers, or with any
intervening rule that targets an overlapping selector, are left alone. If this
condition cannot be established cheaply, ship D1–D4 and leave D5 out — it is an
optimisation, and the correctness items are not.

**D6 — Document the zero-value contract** in `ARCHITECTURE.md` and enforce it
with a test fixture whose provider would produce different output if a field were
read.

---

## 4. The contract `ssr` publishes

To be written into `docs/ARCHITECTURE.md` as the authoritative statement, since
this plan is ephemeral.

A package is collected if it declares at least one method named `RootCSS`,
`RenderCSS`, `RenderHTML`, `RenderJS` or `IconSvg`, on any type, in any non-test
file.

| Method | Goes to | Meaning |
|---|---|---|
| `RootCSS()` | `SSRAssets.RootCSS` | `:root` token declarations. Owned by `tinywasm/css`; an application overrides via `css.Theme(css.Set(…))`. |
| `RenderCSS()` | `SSRAssets.CSS` | Component CSS, scoped to the component. |
| `RenderHTML()` | `SSRAssets.HTML` | Prerendered markup. |
| `RenderJS()` | `SSRAssets.JS` | Scripts. |
| `IconSvg()` | `SSRAssets.Icons` | Sprite, merged across packages. |

Rules the author must satisfy:

1. The provider runs on a **zero value** (`&T{}`). It must not read fields.
2. The provider must be **pure and deterministic**: same input, same bytes.
3. Any number of types per package may declare providers.
4. Declaring none while importing `tinywasm/widget/style` is an error.

---

## 5. Implementation order

Dependency order, not risk order.

1. **`go/ast` provider detection** (D1) — replaces the five method regexes and the
   five function-fallback regexes in `invoke.go`, and the `ssrSourceFiles` list in
   `extract.go`. Reuse `scanner.go`'s mtime cache.
2. **N receivers per package** (D2) — `moduleAlias.ReceiverType string` becomes
   `ReceiverTypes []string`; the `main.go` template loops over them.
3. **Widen the missing-provider error** (D3), now that detection no longer depends
   on filenames.
4. **Layer hoist and dedupe** (D4) in `MergeResultsFor`, plus the conflicting-order
   error.
5. **Identical-block merge** (D5), behind the safety condition. Drop this step if
   the condition cannot be met cheaply.
6. **`ARCHITECTURE.md`**: the contract in §4, the zero-value rule, and the
   three-library boundary table from §1.
7. **`README.md`**: index every file in `docs/` except this one.

Coordination: `tinywasm/widget` is landing a breaking release that removes the
unreachable `.fl-*`/`.exc-*` selectors from every sheet. Steps 4 and 5 are worth
measuring **after** that lands, so the dedupe work is sized against the real
output rather than today's.

---

## 6. Test strategy

Each test maps to a finding, so a regression is a named failure.

| Test | Asserts |
|---|---|
| `TestTwoProvidersOnePackage` | a package with `Alpha` and `Beta` both declaring `RenderCSS()` emits **both** stylesheets — 2.1 |
| `TestProviderOutsideCssGo` | a `RenderCSS()` in `masterdetail.go`, with no `css.go`, is collected — 2.2 |
| `TestProviderMultilineSignature` | a receiver split across lines, and a generic receiver `*Table[T]`, are both detected — 2.5 |
| `TestNoProviderIsAnError` | a package importing `widget/style` with no provider fails the build loudly — D3 |
| `TestSingleLayerStatement` | merged output contains exactly one `@layer …;`, positioned before any rule — D4 |
| `TestConflictingLayerOrderErrors` | two packages declaring different layer orders is an error, not last-one-wins — D4 |
| `TestIdenticalBlocksMerged` | two components using the same primitive emit one rule with both selectors; a counter-fixture with an intervening overlapping selector is **not** merged — D5 |
| `TestZeroValueProvider` | a provider whose output would differ if a field were read still emits the zero-value form — 2.4 |
| existing `deterministic_order_test.go` | byte-for-byte stable merge across runs — keep, and extend to cover D2 and D5 |

`TestIdenticalBlocksMerged` carries the counter-fixture deliberately: a merge
optimisation without a test proving where it *stops* is how cascade bugs get
shipped.

---

## 7. Acceptance criterion

Build a fixture app with two components in one package, a third whose provider
lives in `masterdetail.go` rather than `css.go`, and a fourth that imports
`widget/style` and declares nothing.

Today: the second component ships unstyled, the third ships unstyled, the fourth
ships unstyled — and the build is green three times over.

After this plan: the first three are all collected, and the fourth fails the
build with a message naming the package.
