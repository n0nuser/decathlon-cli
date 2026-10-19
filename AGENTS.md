# decathlon-cli

Unofficial, read-only terminal client for decathlon.es. It reads the storefront's own pages
over a Cloudflare session minted by a local FlareSolverr, and never writes anything to the shop.

## Language

**Product**:
A product group on the site (the "supermodel"), identified by its group id: the number, or for
marketplace sellers the UUID, after `/p/<slug>/` in a product URL.
_Avoid_: item (except as the `product` command's alias), article

**Model**:
One colour of a Product, with its own price and URL (`…m<model_id>`).
_Avoid_: variant, colourway

**SKU**:
One buyable size of a Model. Stock, EAN and online availability belong to SKUs.
_Avoid_: size (that is only the label), variant

**Ref**:
What the CLI prints for a Model so it can be passed back: `GROUP/MODEL` for Decathlon's own
products, `mp/UUID/cN` for marketplace listings, or a full product URL.

**Listing**:
One page of products, from a keyword search (`/es/search`) or a category (`/es/<path>`). They
offer different Facets.

**Facet**:
A filter a Listing offers (brand, store, delivery time, number of berths…), with its Values.
The CLI matches user input against the Facets the page itself returns.
_Avoid_: attribute

**Store**:
A physical Decathlon store from the store locator, identified by its id (`169` = Pamplona). The
store Facet writes the same id padded to five digits (`00169`).

**Stock**:
Whether a Store holds a SKU. It is presence only: the site publishes no quantities.
_Avoid_: inventory, quantity

**Session**:
The Cloudflare cookies plus the user agent FlareSolverr's browser was given, stored in
`session.json` and replayed by `net/http`. It is not an account.
_Avoid_: login, auth

**Flight**:
The React Server Components payload a page streams in `self.__next_f.push` scripts. It is the
API of record (ADR 0002).

**Watch / Check / Event / Sink**:
- A **Watch** is a local, saved target: a Listing or a Product.
- A **Check** fetches the target, diffs it against stored state and produces **Events**.
- **Sinks** are where Events are delivered besides stdout.

## Working in this repo

- `go build ./... && go vet ./... && go test ./...` must be clean before any commit. CI also runs gofmt and staticcheck.
- Tests never touch the network. They parse real pages from `internal/api/testdata/*.html.gz` and drive the CLI against `httptest` fakes of the storefront and FlareSolverr (`internal/cli/cli_test.go`).
- A new page shape means a new captured fixture, not a hand-written one.
- Before committing a fixture, replace `dkt_ecom_user_temporary_id` with the zero UUID, and keep FlareSolverr JSON and HARs out of the repo: they hold cookies.
- Every bug fix lands with a regression test commented `Regression (bug N): …`.
- Live probing is sequential and slow (the client enforces a 2 s gap). Do not parallelise requests to decathlon.es.
- Locate flight data by its keys, never by row id: ids change on every page. The product is the item group whose id is in the URL, not the first one found (bug 1).
- Output contract: data on stdout (JSON default), diagnostics on stderr, exit codes per `docs/cli-spec.md` §7. Adding a field is fine; renaming or removing one is a breaking change.
- `docs-site/` is maintained separately. `make docs` regenerates `docs-site/content/docs/reference/` from the cobra tree; change help text in `internal/cli`, not the generated pages.
- No cart, checkout or account features. See ADR 0003.
