# Handoff report

Branch `feat/initial`, worktree `/home/pablo/.herdr/worktrees/decathlon-cli/feat-initial`.
Version string in the captures below: `07e2398` (`make build`).

## Summary

`decathlon` is an unofficial, read-only Go CLI for decathlon.es: search with every filter the
website offers, read product pages, see which physical stores hold each size, and watch prices and
stock. It reads the storefront's own React Server Components payload over a Cloudflare session that
a local FlareSolverr mints, and never writes anything to the shop. JSON is the default output;
`--format pretty` is for people.

This session finished the handoff list:

1. **docs-site merged.** `feat/docs-site` was merged into `feat/initial` (fast-forward), and
   `cmd/docsgen` writes `docs-site/content/docs/reference/*.mdx` from the cobra tree. The generated
   reference was regenerated and committed. `cd docs-site && npm install && npm run build` passes
   (26 static pages; the only build warnings are Tailwind's own `::highlight` pseudo-element notes).
   `make docs-check` is clean, so the committed reference matches the binary.
2. **The rest of the brief was already in place and was reviewed, not redone:** fixtures + round-trip
   tests for every API type (listing, category listing, product, marketplace product, stores,
   categories) with one regression test per bug; `Makefile`; `.goreleaser.yaml`; all four workflows
   (ci, pages, release, verify-release); the `api-change` issue form wired to the exit-7 link;
   `AGENTS.md`; ADRs 0001–0003; `docs/cli-spec.md`; `docs/decathlon-api-discovery.md`; README.
   The unfinished edits in the handoff commit (README, cli-spec, ci.yml, issue form, skills note,
   `output.go`) were read and kept; the issue-form field ids and the exit-7 link agree.
3. **Second adversarial review pass** over the whole tree, in
   `docs/review-2026-10-19-pass2.md`, with fixes and regression tests (details below).
4. **Literal final outputs** captured at the end of this report.

## Adversarial review, second pass

Five fixes, each with a regression test, plus two smaller ones:

| id | class | finding | regression test |
|---|---|---|---|
| 8 | H | a flight text row's declared byte length was trusted, so a corrupt page made the parser seek to a negative offset and **panic** | `TestFlightRejectsOversizedTextRow` |
| 9 | M | a crafted product/category URL made the client fetch **another host** | `TestFetchRefusesForeignHost` |
| 10 | M | a broken `config.toml` made `doctor` exit 2 before any check, hiding the one failure it exists to show | `TestDoctorReportsBrokenConfig` |
| 11 | M | the scheduler's flat one-minute grace let a five-minute watch fire every four minutes | `TestDue` |
| 12 | M | `mcp install` re-read harness configs through `float64`, changing big ids and precise floats | `TestInstallKeepsNumbers` |
| — | L | `search --page -1` / `--limit -1` were accepted and ignored | `TestExitCodes` |
| — | L | `ntfy` notifications carried a hardcoded tag | live-checked |
| 13 | H | `--page` was a 0-based index while the site, the header and the hint counted from 1, so `--page 2` fetched the third page and page 2 looked skipped | `TestSearchPageNumbering`, `TestWatchSearchPagesAreOneBased`, `TestBuildPageNumbers` |

Documented, not changed: the `watch events --since` string comparison at a second boundary; the
64 KiB challenge-size heuristic; `config set` losing comments when it rewrites the file.

## Verification

```
$ gofmt -l .                      # no output
$ go build ./...                  # ok
$ go vet ./...                    # ok
$ go run honnef.co/go/tools/cmd/staticcheck@latest ./...   # ok
$ go test ./... -count=1
ok  github.com/n0nuser/decathlon-cli/internal/api
ok  github.com/n0nuser/decathlon-cli/internal/cli
ok  github.com/n0nuser/decathlon-cli/internal/mcp
ok  github.com/n0nuser/decathlon-cli/internal/output
ok  github.com/n0nuser/decathlon-cli/internal/updater
$ make docs-check                 # ok
$ (cd docs-site && npm install && npm run build)   # ok
```

**Test count: 40 test functions**, including 15 regression tests named after the bugs in the
review documents (bugs 1–13). Tests replay real captured pages from `internal/api/testdata/*.html.gz`
and drive the CLI against `httptest` fakes of the storefront and FlareSolverr; nothing touches the
network.

## Open caveats

- **FlareSolverr is a hard dependency.** The CLI cannot reach decathlon.es without a local
  FlareSolverr (`http://localhost:8191` by default). `doctor` explains this; a Cloudflare refusal of
  a freshly minted session is exit 5.
- **Stock is presence only.** The storefront publishes no quantities, so no command can report them.
- **A `--store` filter needs a category listing.** Keyword search pages offer no store facet, so
  `search <keywords> --store …` is a usage error (exit 2) by design; `store stock` is the tool for
  one product.
- **`product show GROUP/UNKNOWN_MODEL`** shows the product's default colour, because the site
  redirects; `store stock`/`watch add product` with that ref fail with exit 4.
- **The 2 s request gap is per process.** A cron `watch check` overlapping a manual command can
  briefly double the rate. Watch intervals are ≥ 5 minutes.
- **Cloudflare challenge heuristic:** an interstitial larger than 64 KiB would be taken for a real
  page, surfacing as exit 7 rather than a re-mint.
- **`config set` rewrites the whole file** from the typed schema, so hand-written comments and key
  order are lost on any edit.
- **docs domain.** README links point at `https://n0nuser.github.io/decathlon-cli/` (the GitHub Pages URL from `pages.yml`).
- **Pre-existing commit trailers.** Commits made by the previous agent carry a `Co-Authored-By` /
  `Claude-Session` trailer; they are in already-pushed history and were left alone (no force-push).
  This session's commits carry none.

## Literal outputs
`decathlon doctor` on this machine (FlareSolverr 3.5.2 running locally):

```text
ok  config        /home/pablo/.config/decathlon-cli/config.toml
ok  flaresolverr  http://localhost:8191 (v3.5.2)
ok  session       minted 2026-10-19 08:08, 5 cookies
ok  decathlon.es  home page fetched over net/http with the session
```

### `decathlon --help`

```text
Search decathlon.es with every filter the website has, read product pages,
see which physical stores hold each size, and watch products or searches for
price and stock changes. Read-only: no cart, no account, no checkout.

Output is JSON on stdout by default; --format pretty is for humans.
Diagnostics go to stderr. Requests need a local FlareSolverr (see doctor).

Examples:
  decathlon search "tienda campaña" --format pretty
  decathlon search --category deportes/camping/tiendas-campana --store 169 --max-price 60
  decathlon product show 303295/8513471
  decathlon store stock 303295/8513471 --near 40.42,-3.70 --format pretty
  decathlon watch add product 303295/8513471 --store 169 --name tent

Agent usage: decathlon skills get decathlon-usage

Usage:
  decathlon [command]

Available Commands:
  category    Browse the category tree
  completion  Generate the autocompletion script for the specified shell
  config      Read and change config.toml
  doctor      Check config, FlareSolverr, the session and that decathlon.es answers
  help        Help about any command
  mcp         Serve the read commands to an agent over MCP (stdio)
  product     Read a product page: models, sizes, prices and stock
  search      Search products, or list a category, with the website's filters
  sink        Notification targets declared in config
  skills      Embedded usage notes for agents
  store       Physical stores and which of them hold a product
  update      Update decathlon to the latest stable release
  watch       Watch products and searches for price and stock changes

Flags:
      --debug                 log each request to stderr
      --error-format string   stderr error format: text or json (env DECATHLON_ERROR_FORMAT) (default "text")
      --format string         stdout format: json, jsonl, pretty, toon (default "json")
  -h, --help                  help for decathlon
      --no-color              no colour in pretty output (also NO_COLOR)
      --no-input              never prompt; fail when input is missing
  -v, --version               version for decathlon

Use "decathlon [command] --help" for more information about a command.
```

### `decathlon search "tienda campaña" --format pretty`

```text
1675 products · Tiendas campaña · page 1 of 42
REF                                           PRICE             BRAND     TITLE                                                     RATING      
303295/8513471                                24,99 €           QUECHUA   Tienda de campaña 2 personas Quechua MH100                4.7 (3912)  
157674/8648391                                109,99 €          QUECHUA   Carpa plegable habitáculo 6 personas UPF30 Arpenaz Base…  4.6 (2110)  
177332/8731616                                169,99 €          QUECHUA   Tienda de campaña familiar 4 personas Quechua Arpenaz 4…  4.6 (3280)  
4123/8648383                                  114,99 € (-12%)   QUECHUA   Tienda de campaña familiar 4 personas UPF30 Arpenaz 4.1   4.7 (2036)  
308317/8553542                                44,99 €           QUECHUA   Tienda de campaña 3 personas Quechua MH100                4.7 (2455)  
308178/8554912                                179,99 € (-5%)    QUECHUA   Carpa plegable habitáculo de camping 10 personas UPF50+…  4.7 (936)   
310893/8858233                                119,99 €          SIMOND    Tienda de campaña domo para trekking 2 personas Forclaz…  4.6 (542)   
301558/8882674                                249,99 €          SIMOND    Tienda de campaña domo trekking 2 plazas, ligera y con …  4.5 (678)   
324534/8968614                                189,99 €          SIMOND    Tienda de campaña-lona trekking 2 plazas, ultra ligera …  4.3 (275)   
346717/8801561                                109,99 €          QUECHUA   Tienda de campaña 3 personas Quechua 2 Seconds F&B        4.7 (599)   
313085/8576111                                69,99 €           QUECHUA   Tienda de campaña 3 personas Quechua MH100 F&B            4.8 (3058)  
340058/8784162                                74,99 € (-12%)    QUECHUA   Tienda de campaña 3 personas Quechua 2 Seconds            4.6 (500)   
311103/8562098                                179,99 €          QUECHUA   Tienda de campaña familiar 4 personas UPF30 Arpenaz 4.2   4.5 (491)   
340058/8755227                                74,99 € (-12%)    QUECHUA   Tienda de campaña 3 personas Quechua 2 Seconds            4.6 (500)   
mp/102315d3-95bc-4ded-8fa8-3d8b3111e4bd/c30   34,99 €           INUITZ    Tipi Mini Infantil y para Mascotas - Tienda Campaña 2-6…              mkt
mp/a5d8d195-6a1b-4505-9621-2a2fd42d3e81/c251  41,56 €           AKTIVE    Tienda campaña 4 personas 205x205x130 cm Aktive           5.0 (7)     mkt
342818/8767534                                1790,99 € (-21%)  QUECHUA   Tienda de campaña de techo rígida Fresh&Black de 2 plaz…  4.3 (27)    
348223/8810926                                349,99 €          QUECHUA   Avance para furgoneta 6 personas Quechua Van Connect Fr…  4.5 (100)   
336287/8734851                                34,99 € (-5%)     QUECHUA   Refugio de camping montaje instantáneo 2 personas Quech…  4.5 (1440)  
311272/8858234                                179,99 €          SIMOND    Tienda campaña 3 personas domo de trekking Forclaz MT500  4.2 (192)   
331853/8980286                                89,99 €           QUECHUA   Tienda de campaña 2 personas Quechua MH100 XL F&B         4.6 (174)   
343262/8968612                                149,99 €          SIMOND    Tienda de campaña-lona trekking 1 plaza, ultra ligera y…  4.3 (184)   
157654/8572950                                449,99 €          QUECHUA   Tienda de campaña familiar 4 personas hinchable Quechua…  4.7 (366)   
310022/8561167                                249,99 €          QUECHUA   Tienda de campaña familiar 4 personas UPF50+ Arpenaz 4.…  4.6 (821)   
338778/8975216                                249,99 €          SIMOND    Tienda de campaña domo de trekking 2 plazas - MT900 Min…  4.5 (57)    
331938/8642422                                999,99 € (-20%)   QUECHUA   Tienda de techo Camper 2 personas Quechua Roof Top MH50…  4.6 (450)   
352655/8871289                                149,99 €          QUECHUA   Tienda de campaña instantánea para 3 personas, 2 Second…  4.7 (342)   
334019/8648414                                269,99 € (-40%)   QUECHUA   Tienda campaña familiar 8 personas Quechua Arpenaz 8.4    4.5 (49)    
305777/8882673                                189,99 €          SIMOND    Tienda de campaña domo trekking 1 plaza, ligera y con a…  4.5 (467)   
346745/8801560                                89,99 € (-10%)    QUECHUA   Tienda de campaña 2 personas Quechua 2 Seconds F&B        4.7 (255)   
mp/107feeeb-79a0-4a0b-9f27-f214f0d79010/c251  299,00 €          SKANDIKA  Segunda vita - Tienda de campaña - Tonsberg 10 Protect …              mkt
mp/cb417daa-97d1-4925-9411-2d2883c44ce3/c251  279,00 €          SKANDIKA  Tienda de campaña tipo túnel Isken 5 Protect - cabina d…              mkt
313048/8576110                                219,99 €          QUECHUA   Tienda de campaña 3 personas Quechua 2 Seconds Easy F&B   4.5 (226)   
340106/8786418                                139,99 €          SIMOND    Tienda de campaña domo de trekking 2 personas Forclaz M…  4.4 (107)   
mp/91cf39a4-ac8e-485c-92c0-bf94fb728b4c/c5    53,38 €           AKTIVE    Aktive Tienda de campaña pop up para 2 personas c/doble…  5.0 (2)     mkt
mp/cec4b96f-b576-4635-98bf-83157a1f3e2c/c5    149,00 € (-21%)   SKANDIKA  Tienda campaña - Hammerfest 6 personas - 2 x cabinas - …  2.9 (9)     mkt
308355/8553541                                109,99 €          QUECHUA   Tienda de campaña 2 personas Quechua 2 Seconds Easy F&B   4.7 (969)   
mp/23c1bf1e-004f-4f67-b96e-ba6f3b236ea8/c30   399,00 € (-15%)   SKANDIKA  Tienda de campaña tipo túnel - Hafslo 5 XL Sleeper Prot…  5.0 (1)     mkt
331788/8641816                                289,99 €          SIMOND    Tienda de alpinismo para dos personas Simond Makalu T2    4.6 (21)    
363415/8938357                                649,99 €          QUECHUA   Tienda campaña hinchable 6 personas 3 habitaciones Fres…  4.8 (87)    
more: --page 2
```

### `decathlon store stock 303295/8513471 --near 42.81,-1.64 --limit 4 --format pretty`

```text
Tienda de campaña 2 personas Quechua MH100
ID   STORE                   CITY               DISTANCE  SIZE         COLOUR                  STOCK
169  Pamplona                Berriozar          3.4 km    Talla única  gris/azul petróleo/ve…  in stock
351  Logroño                 Logroño            78.9 km   Talla única  gris/azul petróleo/ve…  in stock
632  Tudela                  Tudela             83.3 km   Talla única  gris/azul petróleo/ve…  in stock
312  Vitoria (C.C. Gorbeia)  Vitoria - Gasteiz  88.1 km   Talla única  gris/azul petróleo/ve…  in stock
the site reports presence only, never quantities
```

### `decathlon store list --near 42.81,-1.64 --limit 4 --format pretty`

```text
ID    STORE          CITY       PROVINCE   DISTANCE
2759  City Pamplona  Pamplona   Navarra    0.5 km
169   Pamplona       Berriozar  Navarra    3.4 km
1080  Estella        Estella    Navarra    36.1 km
194   Irún           Irún       Guipúzcoa  59.0 km
```
