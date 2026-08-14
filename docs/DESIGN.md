# Design decisions — `sitec`

Justifies the decisions behind [ARCHITECTURE.md](ARCHITECTURE.md) and records
what was rejected. Does not restate the architecture and does not specify
behaviour — that is [SPECS.md](SPECS.md).

Read this when a decision looks arbitrary and you need to know whether it can be
changed.

---

## 1. Why detection moves from regex to `go/ast`

**Decision.** Identify producers by parsing every non-test `.go` file in the
package with `go/ast`, matching on method name.

The published detector matches five regular expressions against four hardcoded
filenames. Three failures follow from that, and all three are silent:

- **A producer outside `css.go`, `js.go`, `svg.go` or `html.go` is never seen.**
  Worse, the guard that raises "declares no producer" is keyed on the presence of
  `css.go`, so a producer written in `widget.go` fails *both* checks: not
  detected, and not reported. The loud error only protects the author who already
  got the filename right.
- **Only the first match per pattern is used.** `FindSubmatch` returns one result,
  so a package declaring producers on two types collects the first and drops the
  second entirely.
- **The pattern requires a single-line signature.** A gofmt-legal receiver split
  across lines, or a generic receiver, does not match and yields no assets.

`go/parser` is already a dependency — `scanner.go` uses it to read imports, with
an mtime cache — so the fragility buys nothing.

**Consequence.** The `css.go` convention survives as a style preference rather
than a correctness trap. That is the principle applied deliberately: **delete a
convention rather than document it.** A rule the author must remember in order to
be collected correctly is a rule that will eventually be forgotten, and forgetting
it here is invisible.

**Rejected: keeping the regexes and adding filenames.** Every new filename is
another rule to remember, and it fixes none of the other two failures.

**Rejected: requiring registration via `init()`.** Explicit, and immune to all
three failures — but it is exactly the boilerplate this module exists to avoid,
and a forgotten registration fails just as silently.

**Constraint preserved.** Detection still matches on **method name only**, never
signature or return type. That property is why `IconSvg()` could change its return
type without touching the extractor, and why the generated program can type the
sprite as `any`. `go/ast` makes it easier to hold, not harder.

---

## 2. Why any number of producer types per package

**Decision.** Collect every type declaring a producer; instantiate each;
concatenate in sorted type-name order.

There is currently an implicit "one component per package" rule. It is nowhere
documented and nowhere enforced, and violating it silently drops a component's
assets. Of the three ways out — document the rule, enforce it with an error, or
remove it — removing it is the only one that costs the author nothing.

Sorting by type name keeps emission deterministic, which the cache and the
downstream diffs both depend on.

**Rejected: erroring on more than one type.** Enforces a restriction that has no
technical reason to exist. The generated program can instantiate ten types as
easily as one.

---

## 3. Why the missing-producer error widens

**Decision.** A package that imports an asset-producing library but declares no
producer fails the build, naming the package.

The existing check is right in spirit and too narrow in practice: it triggers on
`css.go` present and no producer. The author who never created `css.go` — the more
likely mistake, and the one the filename convention causes — gets silence.

Keying on the **import** instead of the filename reaches the real condition: this
package pulled in a styling library, so it intended to produce style.

**Rejected: warning instead of failing.** A warning in a green build is how the
empty stylesheet shipped the first time.

---

## 4. Why the cascade-layer statement is hoisted

**Decision.** The merged output carries exactly one layer statement, before any
rule. Conflicting orders are an error.

Sheets are concatenated, and each `widget/style` sheet declares its own layer
order, so the statement repeats once per component. Repetition is harmless in CSS
— the first occurrence establishes the order — but that is precisely the hazard:
the order the application ends up with depends on which sheet happens to be
merged first, and the merge order is a function of package paths. A silent
dependency of the entire cascade on sort order is not acceptable, and if two
packages ever disagree, the disagreement is undetectable by inspection.

Erroring on conflict is the only option that fails at the point where the problem
is fixable.

**Rejected: having `widget/style` stop emitting the statement, and letting
`css.RootCSS()` own it.** Fewer moving parts, but it couples `css` to layer names
that belong to `widget`, and it breaks any consumer that does not use
`css.RootCSS()`. Hoisting is robust regardless of who emits it.

---

## 5. Why identical blocks are merged, and where the merge stops

**Decision.** Merge byte-identical declaration blocks within the same layer into
one rule with a combined selector list, preserving the position of the first
occurrence.

Two components that both use the same layout primitive emit the same declarations
under different selectors. At application scale this is the bulk of the
redundancy, and no individual sheet can remove it.

The safety condition is the important half of this decision. Merging moves
selectors, and moving a selector past an intervening rule that also matches it
changes the cascade. So the merge applies **only** when the blocks are in the same
layer, byte-identical, and no rule between them targets an overlapping selector.

**This is an optimisation, not a correctness fix.** If the safety condition cannot
be established cheaply, ship without it. A merge without a test proving where it
*stops* is how cascade bugs get shipped, which is why the specification requires a
counter-fixture that must **not** merge.

---

## 6. Why the zero-value contract is written down rather than relaxed

**Decision.** Document `&T{}` instantiation as a contract on the author, and test
it.

Producers are called on zero values. A producer that reads a field silently emits
assets built from zeros — no error, plausible-looking output. The alternative
would be constructing instances somehow, which means either a constructor
convention (more boilerplate, more to forget) or reflection over fields the
extractor cannot know how to populate.

The constraint is reasonable in itself: a producer describes what a component
*is*, not what one instance currently contains. Writing it down costs nothing and
makes the failure diagnosable.

---

## Related documents

- [ARCHITECTURE.md](ARCHITECTURE.md) — the structure these decisions produce.
- [SPECS.md](SPECS.md) — the exact behaviour they specify.
