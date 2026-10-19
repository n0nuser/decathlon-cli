# Adversarial review, second pass, 2026-10-19

A fresh pass over the whole tree after the first pass ([review-2026-10-19.md](review-2026-10-19.md))
and before release. Classes are **H** (fix before shipping), **M** (should fix) and **L**
(document). Every fix above the L line lands with the regression test named in the table.

## Fixed

| id | class | finding | fix | regression test |
|---|---|---|---|---|
| bug 8 | H | A flight text row (`id:T<len>,<bytes>`) had its declared byte length trusted. A length that ran past the buffer, or a "negative" hex length, moved the row cursor to a negative offset and `parseRows` panicked with a slice-bounds stack trace, so a corrupt or hostile page crashed the CLI instead of reporting a page it could not read. | The length is bounds-checked against the bytes that remain; a length that is not one stops the walk. | `TestFlightRejectsOversizedTextRow` |
| bug 9 | M | `PRODUCT` and `--category` accept URLs, and `Fetch` fetched whatever host a URL named. A crafted or copied link (`https://evil.example/…`, or a metadata address) turned the client into a fetcher for another site, against the stated contract that it only GETs public decathlon.es pages. | `Fetch` refuses any target whose host is not the storefront's, before a session or a network call. | `TestFetchRefusesForeignHost` |
| bug 10 | M | `doctor` is the command a person runs when something is broken, but `setup` aborted on a bad `config.toml` before any check ran: `doctor` exited 2 with the parse error and never showed the table, so "the config parses" — its first check — was the one failure it could not report. | A config error is now fatal everywhere except `config` and `doctor`; `doctor` shows it as a failed `config` check and carries on with the rest. | `TestDoctorReportsBrokenConfig` |
| bug 11 | M | The scheduler treated a watch as due a flat minute early. For a five-minute watch under a minute-granularity cron that means a check every four minutes — 25% more traffic than the documented five-minute floor, against a site that asks to be treated gently. | The jitter grace shrinks so it never pulls the effective interval below the 5-minute minimum; longer intervals keep the full minute of slack. | `TestDue` |
| bug 12 | M | `mcp install` read a harness config and re-marshalled the whole file through `float64`, so a large numeric id lost precision and a precise float changed, in a file the installer claims to touch only at `command` and `args`. | The file is decoded with numbers left as their original tokens, so untouched values round-trip byte-for-byte. | `TestInstallKeepsNumbers` |
| — | L | `search --page -1` and `--limit -1` were accepted and silently ignored (a negative page is not `> 0`, so it fell through to the unfiltered first page). | Both are refused as usage errors. | `TestExitCodes` |
| — | L | `ntfy` notifications carried a hardcoded tag unrelated to the event. | The tag is the event kind (`out_of_stock`, `price_changed`, …). | live-checked |

## Checked, no change needed

- **`ParseRef` on malformed input** (`""`, `/`, `mp/`, `mp/x/c`, `http://x`, a half ref): every
  case is a usage or not-found error, no panic.
- **`fold`/`slug` on empty and accent-only input**: they return empty, and no caller can pass an
  empty filter value (`--filter` rejects it, `searchFlags.params` validates), so the empty-string
  facet match is unreachable.
- **`stock` model/size narrowing:** `matchedModel`/`matchedSize` are set only when a model/size
  filter is satisfied, so `--model`/`--size` that match nothing stay exit 4.
- **MCP argument handling:** values become `--flag=value`, positionals follow `--`, and unknown or
  mistyped arguments are `-32602`; no argument can become a new flag.
- **`mcp install` on codex TOML:** comments and ordering survive; a decathlon server declared under
  another table is refused rather than duplicated.
- **File modes:** `session.json`, `config.toml` and `state.db` stay 0600, directories 0700.
- **`doctor --mint`:** forgets then mints exactly once; the session check does not double-mint.

## Open (documented)

| class | finding |
|---|---|
| L | `watch events --since` filters with a lexicographic compare on `RFC3339Nano` strings. Within one second, a whole-second timestamp (`…:47Z`) sorts after a fractional one (`…:47.5Z`), so a boundary event can be included or dropped wrongly. Events from a single check share one timestamp and the id ordering is exact, so this only bites on the `--since` edge; not worth a schema migration. |
| L | A challenge larger than 64 KiB is taken for a real page, because the interstitial is assumed small. If Cloudflare grows it, the symptom is exit 7 rather than a re-mint. |
| L | `config set` re-serialises the whole file from the typed schema, so hand-written comments and key order are lost on any edit. Editing in place would need a TOML-aware editor. |

## Follow-up (reviewer finding)

| id | class | finding | fix | regression test |
|---|---|---|---|---|
| bug 13 | H | `--page` was a 0-based index while the site, the header and the more-hint counted from 1: `--page 2` fetched the third page (`from=80`), `--page 1` fetched the second (`from=40`), and the first page was only reachable as `--page 0`. Following the printed page number always landed a page past, so page 2 looked skipped; the JSON `page` field was 0-based too. | `--page`, the header, the more-hint and the JSON `page` are all 1-based page numbers, matching the site. `SearchParams.Page` is 1-based (zero means the first page, so a zero value still costs one request) and builds `from=(page-1)*per`. The search-watch loop, which advances a page number, starts at 1 and breaks on `page >= pages`. | `TestSearchPageNumbering`, `TestWatchSearchPagesAreOneBased`, `TestBuildPageNumbers`, `TestListingKeyword` |

