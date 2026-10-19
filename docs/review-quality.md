# code-quality review: `feat/initial`

Two-rubric review of the whole branch, against `main` (`35765c6`, the empty scaffold):

```
$ git diff main...feat/initial --stat | tail -1
 107 files changed, 17219 insertions(+), 1 deletion(-)
$ git log main..feat/initial --oneline | wc -l
  28
```

Line references are **pre-fix**. Status is `fixed` or `deferred` (with the reason). Nothing in
`main` was touched.

## Rubric 1 — Thermo-Nuclear Code Quality

No file was or is ≥ 1000 lines. The largest is `internal/api/search.go` at 721; after the split
below the largest Go file is `internal/api/search.go` at ~330. The 1k rule is not triggered, so
this axis is about decomposition, boundary leaks, dead/duplicated shapes and ad-hoc branching.

| id | sev | finding | status |
|---|---|---|---|
| T1 | M | `internal/api/search.go` (721) mixes three reasons to change: decoding the wire payload (`wire*`, `ParseListing`, `product()`, lines 17-342), building a listing URL (`SearchParams`, `Build`, `facet`, `value`, 344-661) and text folding (`accents`, `fold`, `slug`, 678-701). Divergent Change, and the largest file on the branch. | fixed: split into `listing.go`, `search.go`, `text.go` |
| T2 | M | `internal/cli/misc.go` (334) holds three nouns — `config` (23-219), `doctor` (221-300) and `skills` (302-334) — while the package's own doc comment says "Each noun has its own file" (`root.go:1-2`) and `docs/cli-spec.md` §12 repeats it. Hard repo-standard violation. | fixed: `config.go`, `doctor.go`, `skills.go` |
| T3 | M | `internal/cli/watch.go` (618) also holds the whole `sink` noun (514-618), same documented standard. | fixed: `sink.go` |
| T4 | M | `SearchResult.Build` is 115 lines with four independent concerns (filters 474-507, price 508-532, sort 533-567, page 568-576) and a tri-state `(url, emptyResult, error)` return. The reader must hold all four in their head to change one. | fixed: `applyFilters`/`applyPrice`/`applySort`/`applyPage` |
| T5 | M | The marketplace-ref rule is duplicated with two different detectors: `search.go:324-328` tests `/p/mp/` in the URL, `product.go:251-257` tests an empty model id; both build `"mp/" + id + "/" + lastSegment`. This is exactly the surface of bug 2. A model's ref should have one constructor. Unifying them exposed that the two rules were **not** equivalent: a marketplace listing tile carries a non-numeric "model id", so the URL is the only reliable signal. `TestMarketplace` now pins the exact ref from the listing and the product page. | fixed: `modelRef()` (URL test) |
| T6 | M | `watch.Change{From, To any}` (`watch.go:75-78`) is only ever built with prices (`watch.go:177,249`), and `sink.Text` decodes it into `any` and formats with `%v` (`sink.go:77-80,97`). Unnecessary `any` hides the real invariant (a price move). | fixed: `From, To float64` |
| T7 | M | The event vocabulary lives in `internal/watch` but `internal/sink` re-spells every type as a raw string (`sink.go:93-111`) and re-declares the payload shapes as four anonymous structs (`sink.go:64-80`). Repeated switch + primitive obsession + a duplicated contract between producer and consumer. | fixed: event model moved to `internal/store` (`event.go`), used by both |
| T8 | M | `api.WriteFileAtomic` (`client.go:300-323`) is a generic filesystem helper exported from the storefront client. `internal/mcp` imports `internal/api` *only* for it (`install.go:110,139,181`), and `config` uses it too. Wrong layer; the MCP package should not depend on the HTTP client. | fixed: `internal/atomicfile` |
| T9 | M | `addWatch(ctx, kind string, target any, …)` (`cli/watch.go:86`) pairs two parameters that must agree and erases the target type; `KindSearch` with a `ProductTarget` compiles and fails at check time. | fixed: `watch.Target` with `Kind()` |
| T10 | L | `landingPage = -1` is smuggled through `Error.Status` (`search.go:223,393,399`), a field documented as an HTTP status, to signal "section page, exit 2". Magic sentinel in a shared type. | fixed: explicit `Error.Landing` |
| T11 | L | `slugOf` returns `"\x00"` as "not a category slug" (`search.go:665-676`) and is compared against `slug(c)`. A sentinel string no reader can infer. | fixed: `(string, bool)` |
| T12 | L | Three copies of the same tables: `api.Sorts` keys, `cli.sortNames()` and the `--sort` help text; likewise `api.DeliveryValues` keys and the `--delivery` completion list. Plus bespoke key-sorters `sortedKeys` (`search.go:592`) and `sortedNames` (`cli/watch.go:611`) where `slices.Sorted(maps.Keys(m))` is canonical. | fixed: one ordered table with `SortID/SortNames/DeliveryID/DeliveryNames`; helpers deleted |
| T13 | L | `categoryTree.Pretty` re-checks `d+1 < t.depth` (`category.go:27`) after `prune` has already truncated the tree to that depth; the check is dead, and the `depth` field exists only for it. | fixed: field and check deleted |
| T14 | L | `jsonOf` (`cli/store.go:16`) is a one-line identity wrapper over `json.Marshal`, used once (`category.go:19`). | fixed: inlined |
| T15 | L | `SortOption.Applied` (`search.go:90,282`) is computed and never read (`Sorts` is `json:"-"`). Dead field. | fixed: removed |
| T16 | L | `dirOf` (`misc.go:155-160`) reimplements `filepath.Dir`, and its `MkdirAll(..., 0o755)` (`misc.go:142`) pre-creates the config directory world-readable, defeating the 0700 the atomic writer would have used. The atomic writer already creates the parent, so both calls are redundant. | fixed: both deleted |
| T17 | L | `Client.Search` evaluates `p.modified()` twice (`search.go:433,444`). | fixed: once |
| T18 | L | `runInProcess` re-derives the command path from argv (`cli/mcp.go:82-89`) while discarding the `cmd` that cobra already returned. | fixed: `cmd.CommandPath()` |
| T19 | L | `Execute` (`root.go:52-83`) and `runInProcess` (`cli/mcp.go:64-92`) separately wire an App + root + storage close. Extracting one runner is possible, but `Execute`'s cancellation path writes bogus `decathlon: interrupted` **text** and returns 130, whereas the MCP path must keep returning a JSON envelope; unifying them needs a mode parameter, which is the kind of flag this rubric dislikes. | deferred: needs a cancellation-policy decision, not a mechanical move |
| T20 | L | `addWatch` inserts the watch (`AddWatch`) then commits the baseline (`CommitCheck`) in two transactions (`cli/watch.go:118-127`). A crash between them makes the next check a silent baseline, so the documented "a crash re-reports events" property does not hold on the add path. Fixing needs insert+commit in one store transaction. | deferred: small crash window, store-API change; recorded in `docs/adr`? no — see note |
| T21 | L | `lat, lng` travel as two bare floats through `config.Location`, `parseNear`, `api.Near` and `haversine`. A `Point` type would stop the swap, but it changes several exported signatures for no behavioural gain. | deferred: judgement call, churn outweighs benefit today |
| T22 | L | `internal/api/client.go` (483) holds transport, session lifecycle, FlareSolverr and cache. It is one concept with clear sections and under the 1k threshold. Splitting `session.go`/`cache.go` is mechanical. | deferred: no size violation; sections are already clear |
| T23 | L | The page-fetch loop (`p.Page++`, break on `r.Page >= r.Pages`) is duplicated in `cli/search.go:169-184` and `watch/watch.go:162-186`. A shared iterator needs a stop hook for `--limit`, which adds a parameter to remove ten lines. | deferred: extraction would add a mode for little gain |
| T24 | L | `sink` type is switched on in `Validate`, `Deliver` and the CLI's display code (`cli/watch.go:560-565`). The display case is feature logic in the CLI; the validation/delivery pair is a tagged union. | fixed the display leak (`sink.Target`); full union deferred (config-schema change) |

## Rubric 2a — Standards (repo-documented, then Fowler smells)

Documented sources: `AGENTS.md`, `README.md`, `docs/cli-spec.md`, `docs/adr/*`, the two existing
`docs/review-*.md`. There is no `CODING_STANDARDS.md` or `CONTRIBUTING.md`. Tooling runs gofmt,
`go vet` and staticcheck, so formatting/unused-import findings are skipped.

| id | sev | type | finding | status |
|---|---|---|---|---|
| S1 | H | repo standard | "Each noun has its own file" (`root.go:1-2`, cli-spec §12) is violated by `cli/misc.go` (config+doctor+skills) and `cli/watch.go` (watch+sink). | fixed (= T2, T3) |
| S2 | M | repo standard | Directories are documented as 0700 (`docs/review-2026-10-19.md`; session/state already are), but `configWrite` creates the config dir 0755 (`misc.go:142`). | fixed (= T16) |
| S3 | M | smell: Duplicated Code | Marketplace ref construction (T5); sort/delivery tables (T12); sink payload structs (T7). | fixed |
| S4 | M | smell: Repeated Switches | Event type switched on raw strings in `sink.Text` while `watch` owns the constants (T7); sink type switched in three places (T24). | fixed (event model); display case fixed, union deferred |
| S5 | M | smell: Feature Envy | `cli`'s `sink list` reaches into `config.SinkConfig.Type` to know how a sink displays (`cli/watch.go:560-565`); that knowledge belongs to `sink`. | fixed: `sink.Target` |
| S6 | M | smell: Divergent Change | `api/search.go`, `cli/misc.go`, `cli/watch.go` (T1-T3). | fixed |
| S7 | L | smell: Data Clumps | `kind + target` (T9, fixed); `lat,lng` (T21, deferred). | partly fixed |
| S8 | L | smell: Primitive Obsession | Event types as raw strings (T7, fixed); watch kinds as `string` — tightened to `store.Kind` as part of T7. | fixed |
| S9 | L | smell: Middle Man | `jsonOf` (T14), `sortedKeys`/`sortedNames` (T12). | fixed |
| S10 | L | smell: Speculative Generality | `Change any` (T6), `SortOption.Applied` (T15); several wire fields are declared but never read (`stores.go:56`, `product.go:79,108`, `search.go:175`). The latter document the payload, so only the computed dead field was removed. | fixed (T6/T15); wire fields noted |
| S11 | L | smell: Mysterious Name | `num` (`search.go:590`) formats a price bound; `landingPage` sentinel (T10). | fixed (T10); `num` kept — it is read at its only two call sites and a rename buys nothing |
| S12 | L | smell: Shotgun Surgery | Adding an event type still touches `store` constants, `sink.Text` and the docs. T7 removes the string duplication but the render switch is inherent. | deferred: inherent to the event set |
| S13 | L | smell: Message Chains | `mo.Currencies.Main.ValueWithTaxes` and the nested wire shapes are inherent to the payload (ADR 0002). | no change |
| S14 | — | smell: Refused Bequest | No inheritance in the codebase. | n/a |

## Rubric 2b — Spec (`docs/cli-spec.md` + `docs/decathlon-api-discovery.md`)

I walked every clause of §3-§13 against the implementation. The command tree, flags, JSON shapes,
exit codes, file modes, caching, rate limits, event types, MCP protocol versions and install
formats all match. Findings:

| id | sev | finding | status |
|---|---|---|---|
| P1 | L | §4 says `mcp install` "writes only `command` and `args` of the `decathlon` entry", but the JSON installer also adds `"type": "stdio"` when absent (`mcp/install.go:101-103`). Scope creep against an explicit "only". It is a harmless, widely expected default for the harnesses tested, and removing it risks a harness that requires it. | deferred: judgement call; the sentence is about not clobbering other keys, but the wording should be clarified |
| P2 | L | §4 `store show`: "a name or city that matches exactly one store", but an exact name match returns the first hit without checking uniqueness (`stores.go:127-135`). No duplicate names exist in the 184-store fixture, so this is latent. | fixed: exact-name matches must be unique |
| P3 | L | §4 search lists the sort values in a fixed order; `Build`'s "cannot sort" error used alphabetical order while the CLI's error used the spec order. Unifying the tables made both use the spec order. | fixed (= T12), cosmetic message order |
| P4 | — | §4 `doctor` says "checks four things … config, sinks, FlareSolverr's banner, and the session … It also fetches the home page"; the output has a line per check, so "four things" is a count of kinds, not lines. No deviation. | no change |
| P5 | — | §7 exit codes: 0/1/2/3-unused/4/5/6/7/130 all reachable and mapped as documented (`errors.go:19-33`, `output.go:244-265`, `root.go:70-73`). | no change |
| P6 | — | §9 precedence, §10 politeness/caching, §11 stack, §12 layout (after T2/T3) and §13 non-goals match. ADR 0003's read-only rule holds: the only non-GETs are to FlareSolverr and user sinks. | no change |

## Summary

- **Rubric 1:** 9 M, 15 L — 18 fixed, 1 partly fixed (T24), 5 deferred (T19-T23).
- **Rubric 2a:** 1 H, 5 M, 7 L (plus S14 n/a); mostly the same findings as rubric 1, by design —
  the repo-standard and smell framings are kept separate as the rubric asks.
- **Rubric 2b:** 0 missing/wrong requirements of consequence, 3 L (2 fixed, 1 deferred), 3 checked-clean.
- **Files ≥ 1000 lines:** none before or after. `api/search.go` (721) was split for divergent-change
  reasons, not size. Largest non-test Go files after: `cli/watch.go` 529, `api/client.go` 460,
  `api/search.go` 431, `api/listing.go` 337.
- **Worst issue:** the documented one-file-per-noun standard was broken by two of the three largest
  CLI files (T2/T3), which is also the clearest divergent-change smell; and the marketplace-ref
  duplication (T5) sat on the code path of a previous bug and had already drifted between the two
  call sites.

## Tests added for this pass

- `atomicfile.TestWriteReplacesAndRestricts`: replacement, 0600 file / 0700 directory, no temp left.
- `sink.TestText` / `sink.TestTarget`: the shared event payloads render as before; sink target
  display now lives in the sink package.
- `api.TestSortAndDeliveryTables`: the single sort/delivery tables and their ids.
- `api.TestFindStoreAmbiguousName`: an exact name that matches two stores is a usage error.
- `api.TestExcerptKeepsValidUTF8`: an error body cut at 300 runes stays valid UTF-8.
- `cli.TestCategoryListDepth`: `--depth 1` has no children, `--depth 3` keeps the tree.
- `cli.TestConfigRoundTrip`: now also asserts the config **directory** is 0700.
- `api.TestMarketplace`: now asserts the exact unified marketplace ref.
