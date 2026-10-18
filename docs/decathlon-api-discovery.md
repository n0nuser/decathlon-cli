# decathlon.es API discovery

Probed 2026-10-19, read-only, sequential requests. Transport: [ADR 0001](adr/0001-flaresolverr-minted-session.md).

## 1. Hosts and routing

- `www.decathlon.es`: a Next.js App Router storefront behind Cloudflare. Every path except `/robots.txt` is challenged without a challenge cookie.
- `contents.mediadecathlon.com`: the image CDN. Not fetched by the CLI.
- `*.shoppingapp.decathlon.com`: the mobile BFF (AWS API Gateway) needs an app `x-api-key`. Not used.

The site exposes no client-side JSON API for catalogue or stock. Each page streams its data as
React Server Components flight rows inside `self.__next_f.push([1,"…"])` scripts. The CLI decodes
those.

**Flight format.**
- Rows are `id:payload\n`. Text rows are `id:T<hexbytes>,<text>`, carry a byte length and have no newline.
- `"$undefined"` stands for undefined, and `"$<row>:<path>"` strings are references.
- Row ids change between pages, so the CLI locates data by key, never by id (`internal/api/flight.go`).

## 2. Auth model

None. Everything the CLI needs is public. The only "session" is the Cloudflare cookie set (ADR 0001).

## 3. Catalogue

### Keyword search: `GET /es/search?Ntt=<terms>`

The grid object holds `totalNumberOfProducts`, `products[]`, `filters{applied,items}`,
`pagination{current,total,nbItemsPerPage}`, `sorts[]` and `search{terms,suggestions}`.
- **Filters:** `facets=<key>:<label>_` pieces, concatenated, e.g. `facets=brandLabel:FORCLAZ_brandLabel:CAMPINGAZ_`.
- **Price:** `Nf=sku.modelLowestPrice|BTWN+<min>+<max>`, read from `range.url{key,pattern}`.
- **Sort:** `sort=asc|desc|discountRate|rating`.
- **Paging:** `from=<n>&size=40`.
- **No store facet.**

Searches that match a category redirect to it: "tienda campaña" lands on
`/es/deportes/camping/tiendas-campana?opeco=…`.

### Category listing: `GET /es/<path>`

The grid has the same shape plus `category`, `breadcrumbItems` and a `store` facet (164 stores on tents).
- **Filters:** `/f-<facet>_<slug>` path segments, e.g. `/f-brand_quechua/f-store_pamplona`.
- **Price:** `?price=from_<min>_to_<max>`.
- **Delivery:** `f-lead-time_ud-0-24` (24 h) or `ud-0-48` (48 h).
- **Sort:** `Ns=priceAscending|priceDescending|discountRateDescending|reviewDescending|newArrivals`.

Store facet ids are 5-digit padded (`00169` = store `169`, Pamplona).

The CLI resolves each filter by matching the user's value against the facet values of the
unfiltered page, and takes the URL fragment the site itself attaches to that value
(`SearchResult.Build`).

### Product: `GET /es/p/<slug>/<group>/<variance>m<model>`

The short form `/es/p/_/<group>/m<model>` redirects to the canonical URL. An unknown product is
a real 404 from Go (FlareSolverr renders it as a 200 home page).

The product is the `itemGroup` whose `id` equals `<group>`. Recommendation and bundle blocks on
the same page carry other item groups (regression `TestProductPicksItsOwnGroup`).
- **Colours:** `skuGroups[]` (models), each with a price in `skuGroupConditions[0].typeTargets[]` (`ecommerce` preferred). Fields read: `valueWithTaxes`, `referenceValueWithTaxes`, `discountPercentage`.
- **Sizes:** `skus[]`, each with `sizeLabel`, `gtins[]`, `isAvailable`, `isStorePickupAvailable`, `isHomeOrStoreDeliveryAvailable`.
- **Store stock:** `storesWithStock[]` is a list of store ids per SKU. **No quantities are exposed anywhere.**

### Stores: `GET /es/store-locator`

184 entries, each with `{id, thirdsId, name, coordinates{lat,lng}, address{street, city, postalCode, administrativeAreaLevel1/2}, phoneNumber, workingHours[], holidayHours[]}`.
`id` is the id used in `storesWithStock`. All 61 ids on the MH100 page resolve.

### Categories

`menuItem` objects in the header of any page: `{name, url, primaryCategories[], secondaryCategories[], thematicCategories[]}`, each node with `children[]`.

## 4. Cart, region, account

Out of scope. The CLI is read-only by design.

## 5. Repro

The pages behind each fixture are listed in `internal/api/fixtures_test.go` (`fixtureURLs`).
`go test ./internal/api` replays them with no network.
