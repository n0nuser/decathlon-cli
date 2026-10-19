# 0002. The page's flight payload is the API of record

Date: 2026-10-19
Status: accepted

## Context

decathlon.es is a Next.js App Router site. Probing found no client-side JSON endpoint for
catalogue, product or stock. Search, listings, product groups (with per-SKU
`storesWithStock`) and the store locator are all rendered on the server and streamed inside
the HTML as React Server Components "flight" rows. The mobile-app BFF exists but needs an app
key (see the feasibility doc).

## Decision

- Fetch the same HTML a browser gets, extract the `self.__next_f.push([1,"…"])` chunks, and decode the rows (`internal/api/flight.go`). Text rows are length-prefixed in bytes and have no trailing newline. `"$undefined"` becomes `null`.
- Locate data by keys, never by row id:
  - a listing: `totalNumberOfProducts`, `products`, `filters`;
  - a product: the `itemGroup` whose `id` is the group in the URL;
  - stores: entries with `thirdsId` and `coordinates`;
  - the menu: `menuItem`.
- Build filtered listing URLs from the fragments the page attaches to each facet value, not from templates. Only the price pattern (`range.url`) and pagination (`from`, `size`) are filled in.
- Map unexpected shapes to exit 7 with a prefilled issue link, and cover each shape with a gzipped real fixture.

## Consequences

- Each request downloads ~0.5–1 MB of HTML. Requests are slow and rare by design (ADR 0001), and the static parts are cached.
- A redesign of the storefront breaks parsing loudly (exit 7), never silently: every decoder requires the keys it reads.
- Stock is what the page shows, store presence per SKU. Quantities are not available.
