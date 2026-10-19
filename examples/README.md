# examples

ready-to-run recipes for `decathlon`. everything prints json unless you add
`--format pretty`, so the same lines pipe into `jq` or a script.

the cli needs a local [flaresolverr](https://github.com/FlareSolverr/FlareSolverr)
on `http://localhost:8191` (see the [install](../README.md#install) section), and
reads decathlon.es read-only: no cart, no account.

## first run

```bash
decathlon --version
decathlon doctor --format pretty      # config, solver, session, reachability
decathlon search "tienda campaña" --limit 5 --format pretty
```

## search

```bash
# keywords, with the filters the website has
decathlon search "hornillo gas" --brand campingaz --max-price 40 --sort price_asc --format pretty

# a category listing, filtered to one store and a delivery window
decathlon search --category deportes/camping/tiendas-campana --store 169 --delivery 48h --max-price 60

# what filters this page offers, and their values
decathlon search filters --category deportes/camping/tiendas-campana --format pretty

# any other filter, by key=value from `search filters`
decathlon search --category deportes/camping/tiendas-campana --filter "Nº de plazas=2 plazas"

# one product per line, cheapest first, then take the refs
decathlon search --category deportes/camping/tiendas-campana --sort price_asc --format jsonl |
  jq -r 'select(.price < 60) | .ref'
```

products are addressed by the `ref` that `search` prints: `GROUP/MODEL`
(`303295/8513471`) for decathlon's own products, `mp/UUID/cN` for marketplace
sellers. a product url works too.

## stock

`store stock` is the point of the tool: which physical stores hold a size.

```bash
# every store holding any size of a product, nearest first
decathlon store stock 303295/8513471 --near 40.4168,-3.7038 --limit 10 --format pretty

# only the stores you care about, including the sizes they do not hold
decathlon store stock 336529/8668913 --size 41 --store 169 --store Badalona --all --format pretty

# just the store names that have it in stock
decathlon store stock 303295/8513471 --format jsonl | jq -r 'select(.in_stock) | .store_name'
```

the site reports presence only, never quantities. `store list` shows the ids;
`store show 169` shows one store's address and hours.

## watches

a watch saves a target, checks it on a schedule and emits events on change.

```bash
# one product: price, online availability and the stock of your stores
decathlon watch add product 303295/8513471 --store 169 --name tent --notify phone

# a whole listing: new products and price changes
decathlon watch add search --category deportes/camping/tiendas-campana --store 169 --max-price 60 --name cheap-tents

decathlon watch list --format pretty
decathlon watch check --all --format jsonl | jq -r 'select(.type=="store.in_stock") | "\(.store.name): \(.product.title)"'
decathlon watch events --since 24h --format pretty
decathlon watch remove tent --yes
```

the first check is a quiet baseline; pass `--emit-initial` to report what exists
now. event types: `product.new`, `product.price_changed`, `product.removed`,
`sku.new`, `sku.back_online`, `sku.sold_out_online`, `store.in_stock`,
`store.out_of_stock`.

no scheduler is installed. run `watch check` from cron or a systemd timer:

```cron
*/30 * * * * decathlon watch check --format jsonl >> ~/decathlon-events.jsonl 2>> ~/decathlon-watch.log
```

or keep `decathlon watch run --interval 30m` in the foreground.

## config and sinks

`decathlon config path` prints where everything lives. copy
[config.toml](config.toml) there, or set keys one at a time:

```bash
decathlon config set stores '["169", "86"]'
decathlon config set location.lat 42.8125
decathlon config set location.lng -1.6458
decathlon config set sinks.phone.type ntfy
decathlon config set sinks.phone.url https://ntfy.sh
decathlon config set sinks.phone.topic my-decathlon-alerts
decathlon sink test phone
```

## doctor

```bash
decathlon doctor --format pretty   # what is broken, check by check
decathlon doctor --mint            # discard the session and mint a new one
decathlon --debug store list       # one line per request on stderr
```

still failing? see [docs/troubleshooting.md](../docs/troubleshooting.md).
