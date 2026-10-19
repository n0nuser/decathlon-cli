# decathlon-cli spec

Status: v0.1 draft, 2026-10-19. Endpoint facts are in [decathlon-api-discovery.md](decathlon-api-discovery.md),
the transport decision is in [ADR 0001](adr/0001-flaresolverr-minted-session.md), and the
vocabulary is in [AGENTS.md](../AGENTS.md).

## 1. Name

Binary `decathlon`, module `github.com/n0nuser/decathlon-cli`, Go 1.26, one static binary (`CGO_ENABLED=0`).

## 2. One-liner

Search decathlon.es, see which stores hold each size, and watch prices and stock, from the terminal.

## 3. Usage

```
decathlon [global flags] <noun> <verb> [args] [flags]
```

The CLI uses noun-verb throughout. `search` takes its keywords directly, because searching
is the main use.

## 4. Command tree

```
search     [keywords...] | filters
product    show | open                     (alias: item)
category   list | show
store      list | show | stock
watch      add (search|product) | list | remove | check | run | events
sink       list | test
config     path | list | get | set | unset
doctor
update
mcp        (serve) | install claude-code|codex|cursor
skills     list | get
completion bash|zsh|fish|powershell
```

The tool is read-only by design. Cart, checkout, account and login don't exist, and nothing
public needs a session beyond the Cloudflare cookie.

### search

`search [keywords...] [--category PATH] [--brand B]... [--store S] [--delivery 24h|48h]
[--min-price N] [--max-price N] [--sort relevance|price_asc|price_desc|discount|rating|newest]
[--filter key=value]... [--page N] [--pages N] [--limit N]`

- **Keywords** load `/es/search?Ntt=…`. The site may redirect to a category, and the CLI then works from the URL it was served.
- **`--category`** loads `/es/<path>`.
- **Filter resolution.** Filters are matched, by id, label or slug and ignoring case and accents, against the facets of the unfiltered page, which is cached for 10 minutes. The CLI then builds the URL from the fragments the site attaches to each value. Any filter the website shows therefore works, and `search filters` lists them.
- **Store and delivery** facets exist only on category listings. On a keyword page they are a usage error (exit 2).
- **A store that holds none of the listed products** is not offered as a facet value by the site, so the CLI returns an empty listing.
- **JSON:** `{query, category, url, total, page, pages, per_page, applied_filters[], products[], suggestions[]}`.
- **Each product:** `{ref, id, model_id, sku_id, title, brand, price, currency, reference_price, discount_percent, rating, reviews, available_online, marketplace, seller, sizes[], url, image}`.
- **`ref`** is what `product show`, `store stock` and `watch add product` accept: `GROUP/MODEL` for Decathlon's own products, `mp/UUID/cN` for marketplace sellers.
- **`--format jsonl`** prints one product per line.

`search filters [keywords...] [--category PATH]` lists the facets (`id`, `label`, `kind`, values with counts, price range).

### product

- `product show PRODUCT` returns `{id, title, brand, description, rating, reviews, url, image, breadcrumbs[], models[]}`.
  - Each model: `{ref, model_id, colors[], selected, price, currency, reference_price, discount_percent, seller, marketplace, url, skus[]}`.
  - Each SKU: `{sku_id, size, price, available_online, home_delivery, store_pickup, stores_with_stock[], ean}`.
- An unknown product is exit 4. A bare group id without a model is exit 2: the site has no group-only route.
- `product open PRODUCT` opens the page in the browser.

### category

- `category list [--depth N]` shows the navigation menu tree `{id, name, path, children[]}`, cached for 24 h.
- `category show PATH` returns `{category, total, url, subcategories[{name, path, count}]}`. Landing pages, which have no product list, take their subcategories from the menu.

### store

- `store list [--near LAT,LNG] [--city TEXT] [--limit N]`: the store locator (184 stores), cached for 24 h. `--near` (default: config `location`) sorts by distance and adds `distance_km`.
- `store show STORE`: address, phone, coordinates and weekly hours. STORE is an id (padded or not) or a name or city that matches exactly one store.
- `store stock PRODUCT [--store S]... [--model ID] [--size S] [--near LAT,LNG] [--all] [--limit N]`
  - Returns `{product_id, title, url, lines[{store_id, store_name, city, model_id, color, sku_id, size, in_stock, distance_km}], unknown_stores[]}`.
  - Without `--store` (default: config `stores`), it lists every store holding a size.
  - With `--store`, it checks only those stores, and `--all` adds the sizes they lack (`in_stock: false`).
  - `GROUP/MODEL` narrows to that colour. A URL checks every colour.
  - **Stock is presence only:** the site never exposes quantities.

### watch

- `watch add search [search flags] [--pages 1-5] --name NAME [--notify SINK]... [--interval DUR] [--emit-initial]`
- `watch add product PRODUCT [--store S]... [--model ID] [--size S] --name NAME [--notify SINK]... [--interval DUR] [--emit-initial]`
- Add mode:
  - The first check runs before the watch is saved, so a target that doesn't resolve is refused rather than stored broken.
  - The first check is a silent baseline unless `--emit-initial` is given.
  - Without `--interval`, config `watch.interval` applies, else 30m; nothing below 5m is accepted.
- Other verbs:
  - `watch list` lists watches.
  - `watch remove NAME [--yes]` deletes a watch and its events.
  - `watch check [NAME...] [--all]` checks the due watches (or the named ones, or all), prints their events, delivers them to sinks, then commits state.
  - `watch run [--interval DUR]` runs the same check in a foreground loop, each sleep stretched by a random tenth at most.
  - `watch events [NAME] [--since DUR] [--limit N]` reads stored events.
- Event types:
  - `product.new`, `product.price_changed`: search watches.
  - `product.price_changed`, `product.removed`, `sku.new`, `sku.back_online`, `sku.sold_out_online`, `store.in_stock`, `store.out_of_stock`: product watches.
  - Store events cover the watch's stores. With none given, they cover every store.
- Event shape:

```json
{"type":"store.out_of_stock","watch":"tent","at":"2026-10-19T08:11:47Z",
 "product":{"id":"303295","model_id":"8513471","title":"…","price":24.99,"currency":"EUR","url":"…"},
 "sku":{"sku_id":"4ea60a8f-…","size":"Talla única","color":"gris"},
 "store":{"id":"169","name":"Pamplona","city":"Berriozar"}}
```

- A failing watch is reported on stderr while the others still run. The command exits non-zero at the end.
- State is committed only after delivery, so a crash re-reports events instead of losing them.
- Search watches don't report products dropping off the watched pages, because rankings churn.
- No scheduler service is installed. Use cron or a systemd timer around `watch check` (see `watch check --help`).

### sink

Sinks are declared in config, with type `ntfy` (`url`, `topic`, `token_file`), `webhook`
(`url`, and `template` set to `discord`, `slack`, or unset for the raw event) or `exec`
(`command` array; the event JSON goes to stdin, with `DECATHLON_EVENT_TYPE` and
`DECATHLON_WATCH` in the environment).

- `sink list` shows each sink and any config problems.
- `sink test NAME` sends a sample `store.in_stock` event.

### config / doctor / update / mcp / skills

- **config:** `config path | list | get KEY | set KEY VALUE | unset KEY` edits `config.toml` with dotted keys. Values are validated against the schema before writing, so an unknown key is exit 2. Negative numbers are values, not flags.
- **doctor** checks four things and exits 1 if any fails: config, sinks, FlareSolverr's banner, and the session (stored, or minted now). It also fetches the home page over `net/http` with that session. `--mint` discards the session first.
- **update:**
  - `update [--check] [--yes]` finds the GitHub latest release and verifies its archive against `checksums.txt` before replacing the binary.
  - Dev builds and package-manager installs are refused.
  - Interactive commands offer an update at most once a day. This is skipped for `watch`, `mcp`, `update` and `completion`, in scripts, and with `DECATHLON_NO_UPDATE_CHECK=1`.
- **mcp:**
  - Serves newline-delimited JSON-RPC 2.0 on stdio (protocol 2024-11-05, 2025-03-26 and 2025-06-18).
  - Tools: `search`, `product_show`, `store_list`, `store_stock`, `category_list`, `watch_list`, `watch_check`.
  - Each tool is a CLI argv run in-process with JSON output, so it returns what the command prints. A failing command returns its output followed by the JSON error envelope. Unknown or mistyped arguments are a `-32602` error.
  - `mcp install claude-code|codex|cursor` writes only `command` and `args` of the `decathlon` entry. Other entries and keys are kept. Codex TOML is edited as text, so comments survive.
- **skills:** `skills list | get NAME` prints the embedded agent notes.

## 5. Global flags

| global flag | default | effect |
|---|---|---|
| `--format json\|jsonl\|pretty\|toon` | `json` | stdout rendering; `jsonl` is one element per line |
| `--error-format text\|json` | `text` | stderr errors (env `DECATHLON_ERROR_FORMAT`) |
| `--no-color` | off | also `NO_COLOR`, `TERM=dumb`, non-TTY |
| `--no-input` | off | never prompt |
| `--debug` | off | one stderr line per request (URL, status, size, time) |
| `-h`, `-v/--version` | | |

## 6. I/O contract

- stdout carries results and nothing else: a single JSON value, or with `jsonl` one value per line.
- stderr carries errors, notices (such as a session re-mint) and debug lines.
- The CLI asks questions only when all three standard streams are terminals and `--no-input` is unset.
- Text that comes from the storefront is stripped of control characters before pretty output.

## 7. Exit codes

| exit | when |
|---|---|
| 0 | it worked; a check with no changes counts |
| 1 | generic failure; doctor found problems; some watches failed |
| 2 | usage: bad flags, unknown filter or value, store filter on a keyword search, landing page |
| 3 | reserved for auth (unused: the CLI has no accounts) |
| 4 | not found: product, store, watch, model or size |
| 5 | blocked: Cloudflare refused a freshly minted session, or HTTP 429 |
| 6 | network: decathlon.es or FlareSolverr unreachable, or HTTP 5xx |
| 7 | the storefront changed shape (prints a prefilled issue link) |
| 130 | interrupted |

## 8. Errors

Text errors are one line on stderr, prefixed `decathlon:`, with a hint where one helps.

`--error-format json` prints `{code, category, retryable, message, http_status, endpoint,
suggested_commands, issue_url}`.

Exit 7 links to `.github/ISSUE_TEMPLATE/api-change.yml`, filled in with the build version, platform,
the command's name (never its arguments), the page path, the status code and what the parser expected.

## 9. Config, state, env

| file | location | permissions |
|---|---|---|
| config | `$XDG_CONFIG_HOME/decathlon-cli/config.toml` (or `DECATHLON_CONFIG`) | 0600 (webhook URLs are secrets) |
| session | `$XDG_CONFIG_HOME/decathlon-cli/session.json` | 0600, atomic |
| watch state | `$XDG_DATA_HOME/decathlon-cli/state.db` (SQLite) | 0600 |
| cache | `$XDG_CACHE_HOME/decathlon-cli/` | 0600 files |
| update stamp | `$XDG_STATE_HOME/decathlon-cli/update-check` | |

```toml
flaresolverr_url = "http://localhost:8191"
stores = ["169", "86"]

[location]
lat = 42.8125
lng = -1.6458

[watch]
interval = "30m"

[sinks.phone]
type = "ntfy"
url = "https://ntfy.sh"
topic = "my-decathlon-alerts"
```

Environment variables:
- `FLARESOLVERR_URL`, `DECATHLON_CONFIG`, `DECATHLON_ERROR_FORMAT`, `DECATHLON_NO_UPDATE_CHECK`, `NO_COLOR`.
- For tests and fakes: `DECATHLON_BASE_URL` and `DECATHLON_REQUEST_GAP` (Go duration).

Precedence is flags, then env, then config.

## 10. Politeness and safety

- Requests are sequential, at least 2 s apart. Watch intervals are at least 5 minutes.
- The store directory and menu are cached for 24 h, filter-resolution pages for 10 min. Search results and product pages are never cached.
- The CLI never sends anything to decathlon.es but GETs of public pages, and never phones home. The update check asks GitHub, and only for interactive use.
- `watch remove` asks for confirmation, or needs `--yes`.

## 11. Stack

cobra + pflag, `net/http` (no TLS-fingerprint client, no headless browser in the binary),
`pelletier/go-toml/v2`, `modernc.org/sqlite`, `adrg/xdg` and `toon-format/toon-go`, released with
goreleaser 2. Tests use stdlib `testing` with recorded pages and `httptest` fakes of the
storefront and FlareSolverr. There are no mocks and no network.

## 12. Layout

```
cmd/decathlon/       entrypoint
cmd/docsgen/         command reference for docs-site, from the cobra tree
internal/api/        transport (session, cache, throttle), flight parser, search/product/stores/categories
internal/cli/        one file per noun; skills/ holds the embedded agent notes
internal/config/     XDG paths, config.toml
internal/output/     json/jsonl/pretty/toon, error envelope, exit codes
internal/store/      SQLite: watches, seen state, events
internal/watch/      one check per watch kind, diffing, events
internal/sink/       ntfy, webhook, exec
internal/mcp/        JSON-RPC over stdio, the tool list, harness config writers
internal/updater/    GitHub release check and verified self-replace
docs/                this spec, API discovery, ADRs
```

## 13. Not in v0.1

- Not offered: a cart, checkout, account login, stock quantities (the site doesn't publish them), and a store filter on keyword searches (the site doesn't offer one there).
- Deferred: `watch service install` (cron or a systemd timer covers it), Homebrew/Scoop/AUR/npm packages, and other Decathlon countries.
