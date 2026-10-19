# decathlon: usage for agents

Read-only CLI for decathlon.es: search, product pages, physical stores, per-store stock,
and local watches. It never touches carts, accounts or checkout.

## Before anything

Requests need FlareSolverr running (default `http://localhost:8191`, override with
`FLARESOLVERR_URL`). `decathlon doctor` checks it. Exit 6 means FlareSolverr or the network
is unreachable. Exit 5 means Cloudflare refused even a fresh session, so wait and retry.

## Contract

- stdout holds data, JSON by default. `--format jsonl` prints one element per line. `--format pretty` is for humans only.
- stderr holds diagnostics. Add `--error-format json` for an envelope: `{code, category, retryable, message, ...}`.
- Exit codes: 0 ok, 1 generic, 2 usage, 4 not found, 5 blocked or rate limited, 6 network or FlareSolverr, 7 the storefront changed shape, 130 interrupted.
- Requests are slow on purpose: about 2 s apart, a few seconds each. Prefer one well-filtered call over many.

## Finding products

```sh
decathlon search "tienda campaña"                    # keyword search; may redirect to a category
decathlon search "hornillo gas" --brand campingaz --max-price 40 --sort price_asc
decathlon category list --depth 3                    # paths for --category
decathlon search --category deportes/camping/tiendas-campana --store 169 --delivery 48h
decathlon search filters --category deportes/camping/tiendas-campana   # every filter key/value
```

Each product has `id` (group) and `model_id` (colour). Pass `id/model_id` to `product show`
and `store stock`. `--store` and `--delivery` exist only on category listings. On a keyword
search they are a usage error (exit 2).

## Stock

```sh
decathlon store list --near 40.4168,-3.7038 --limit 5
decathlon store stock 303295/8513471 --near 40.4168,-3.7038     # stores holding each size
decathlon store stock 336529/8668913 --size 41 --store 169 --all # yes/no for one store
```

Stock is presence only. `in_stock` means the store is listed as holding that size. There are
no quantities.

## Watches

```sh
decathlon watch add product 303295/8513471 --store 169 --name tent
decathlon watch add search "hornillo gas" --max-price 30 --name stoves
decathlon watch check --format jsonl
```

Event types: `product.new`, `product.price_changed`, `product.removed`, `sku.new`,
`sku.back_online`, `sku.sold_out_online`, `store.in_stock`, `store.out_of_stock`. The first
check is a silent baseline unless `--emit-initial` is passed.
