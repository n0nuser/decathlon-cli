<p align="center">
  <img src=".github/assets/decathlon-cli-logo.svg" width="560" alt="decathlon cli logo">
</p>

<p align="center">
  <a href="https://github.com/n0nuser/decathlon-cli/releases"><img src="https://img.shields.io/github/v/release/n0nuser/decathlon-cli?display_name=tag&style=flat-square&label=release&color=000000" alt="release badge"></a>
  <a href="https://github.com/n0nuser/decathlon-cli/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/n0nuser/decathlon-cli/ci.yml?branch=main&style=flat-square&label=ci&color=000000" alt="ci badge"></a>
  <a href="https://pkg.go.dev/github.com/n0nuser/decathlon-cli"><img src="https://img.shields.io/badge/go-reference-000000?style=flat-square" alt="go reference badge"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-mit-000000?style=flat-square" alt="license badge"></a>
</p>

---

`decathlon` is an unofficial terminal cli for decathlon.es. you can search with every filter the website has, read product pages, and see which physical stores hold each size. you can also watch products and searches for price and stock changes. it is read-only: no cart, no account, no checkout. it is a single static go binary; the only thing it needs beside it is a local flaresolverr. it prints json unless asked otherwise, ready for `jq`, and `--format pretty` gives tables for a person to read.

[documentation](https://n0nuser.github.io/decathlon-cli/docs) | [command reference](https://n0nuser.github.io/decathlon-cli/docs/reference) | [examples](examples/README.md) | [github](https://github.com/n0nuser/decathlon-cli)

<p align="center">
  <img src="images/demos/search.gif" width="640" alt="decathlon search demo">
</p>

## for AI agents

register the read-only commands as mcp tools, or read the notes bundled for agents:

```bash
decathlon mcp install claude-code     # also: codex, cursor
decathlon skills get decathlon-usage  # or: decathlon skills list
decathlon search "tienda campaña" --limit 1   # smoke: confirms the wiring end to end
```

the server exposes `search`, `product_show`, `store_list`, `store_stock`, `category_list`, `watch_list` and `watch_check`; each returns the command's json.

## install

the one-liner (linux and macos) resolves the latest release, picks your os and arch, verifies the archive against `checksums.txt` and installs `decathlon` to `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/n0nuser/decathlon-cli/main/scripts/install.sh | sh
```

pin a release with `VERSION` (the tag without the leading `v`), and change the target directory with `DECATHLON_INSTALL_DIR` or `PREFIX` (`$PREFIX/bin`). every path:

| path | command |
|---|---|
| curl \| sh | `curl -fsSL https://raw.githubusercontent.com/n0nuser/decathlon-cli/main/scripts/install.sh \| sh` |
| go install | `go install github.com/n0nuser/decathlon-cli/cmd/decathlon@latest` |
| npx (npm wrapper in [`npm/`](npm), published with npm Trusted Publishing) | `npx decathlon-cli search "tienda campaña"` |
| release binary | `decathlon_<version>_<os>_<arch>.tar.gz` from [releases](https://github.com/n0nuser/decathlon-cli/releases), verified below |
| from source | `git clone https://github.com/n0nuser/decathlon-cli && cd decathlon-cli && make install` |

release binaries: download the archive **and** `checksums.txt`, verify, then extract:

```bash
# resolve the latest tag (or set version=1.2.3 by hand)
version=$(curl -fsSL https://api.github.com/repos/n0nuser/decathlon-cli/releases/latest |
  sed -n 's/.*"tag_name": *"v\?\([^"]*\)".*/\1/p')
os=linux arch=amd64        # or darwin / windows, arm64
base="https://github.com/n0nuser/decathlon-cli/releases/download/v${version}"
curl -fsSLO "$base/decathlon_${version}_${os}_${arch}.tar.gz"
curl -fsSLO "$base/checksums.txt"          # download first
sha256sum --ignore-missing -c checksums.txt  # then verify
gh attestation verify checksums.txt --repo n0nuser/decathlon-cli
tar xzf "decathlon_${version}_${os}_${arch}.tar.gz" decathlon
install -m 755 decathlon ~/.local/bin/
```

publishing to npm runs in [`.github/workflows/npm-publish.yml`](.github/workflows/npm-publish.yml) with npm Trusted Publishing (OIDC): on a `v*` tag it syncs the version from the tag and runs `npm publish --access public` with no token. one npm-side step is left for the account owner: after creating the package on npmjs.com, open it → **Settings → Trusted Publishers → add a GitHub Actions trusted publisher** with owner `n0nuser`, repository `decathlon-cli`, workflow `npm-publish.yml`, environment empty. until that exists the workflow fails; it is separate from the binary `release.yml`, so releases are unaffected.

homebrew, scoop and aur are ready but commented out in [`.goreleaser.yaml`](.goreleaser.yaml), because enabling them without these prerequisites would break `release.yml`:

- homebrew: a public `homebrew-tap` repository and the `HOMEBREW_TAP_GITHUB_TOKEN` secret.
- scoop: a public `scoop-bucket` repository and the `SCOOP_BUCKET_GITHUB_TOKEN` secret.
- aur: an AUR account with the `decathlon-cli-bin` package and the `AUR_SSH_PRIVATE_KEY` secret.

## why

when you are buying gear, the real question is often not "how much" but "which store near me has it in my size, today". the website answers that one product and one store at a time. this cli answers it in one command: for a whole listing filtered to a store, or for every store holding each size of a product. it can keep checking for you and tell you when a size lands in your store or a price drops. scripts, agents (there is an mcp server) and shell pipelines all read the same json.

## quickstart

decathlon.es sits behind a cloudflare challenge. the cli asks a local [flaresolverr](https://github.com/FlareSolverr/FlareSolverr) to open the site once in a real browser. it then reuses the cookies that browser was given for its own plain http requests, and asks flaresolverr again only when cloudflare stops accepting them. see [adr 0001](docs/adr/0001-flaresolverr-minted-session.md).

### 1. start flaresolverr

```bash
docker run -d --name flaresolverr -p 127.0.0.1:8191:8191 --restart unless-stopped \
  ghcr.io/flaresolverr/flaresolverr:latest
```

it listens on `http://localhost:8191`, which is the default. point the cli elsewhere with `FLARESOLVERR_URL` or `decathlon config set flaresolverr_url URL`. tested with flaresolverr 3.5.2.

### 2. install the binary

see [install](#install). the short version:

```bash
curl -fsSL https://raw.githubusercontent.com/n0nuser/decathlon-cli/main/scripts/install.sh | sh
```

### 3. check it works

```bash
decathlon --version
decathlon doctor --format pretty
```

`doctor` checks five things: the config parses, any sinks are valid, flaresolverr answers, a session exists (or can be minted), and decathlon.es answers a plain request with it. if one of them fails, see [troubleshooting](docs/troubleshooting.md).

### 4. use it

```bash
decathlon search "tienda campaña" --format pretty
decathlon store list --near 40.4168,-3.7038 --limit 5 --format pretty
decathlon store stock 303295/8513471 --near 40.4168,-3.7038 --format pretty
```

## what it looks like

```text
$ decathlon store stock 303295/8513471 --near 42.81,-1.64 --limit 4 --format pretty
Tienda de campaña 2 personas Quechua MH100
ID   STORE                   CITY               DISTANCE  SIZE         COLOUR                  STOCK
169  Pamplona                Berriozar          3.4 km    Talla única  gris/azul petróleo/ve…  in stock
351  Logroño                 Logroño            78.9 km   Talla única  gris/azul petróleo/ve…  in stock
632  Tudela                  Tudela             83.3 km   Talla única  gris/azul petróleo/ve…  in stock
312  Vitoria (C.C. Gorbeia)  Vitoria - Gasteiz  88.1 km   Talla única  gris/azul petróleo/ve…  in stock
the site reports presence only, never quantities
```

## command surface

| command | what it does |
|---|---|
| `search [keywords] [filters]` | keyword search or `--category` listing with brand, store, delivery, price, sort and any `--filter key=value` the page offers |
| `search filters` | the filters and values a search or category offers |
| `product show / open` | every colour and size, prices, online availability, how many stores hold each size |
| `category list / show` | the category tree and the paths `--category` takes |
| `store list / show` | the 184 stores, nearest first with `--near` |
| `store stock PRODUCT` | which stores hold each size; `--store` to check yours |
| `watch add search\|product` | save a watch; its first check is a quiet baseline unless `--emit-initial` |
| `watch check / run / list / remove / events` | one-shot check, foreground loop, history |
| `sink list / test` | show the configured notification targets, send one a sample |
| `config path / list / get / set / unset` | config.toml |
| `doctor` | config, flaresolverr, session, reachability |
| `update` | checksum-verified self-update from github releases |
| `mcp` / `mcp install claude-code\|codex\|cursor` | the read commands as mcp tools for agents |
| `skills list / get` | usage notes bundled for agents |
| `completion bash\|zsh\|fish\|powershell` | completion scripts |

global flags: `--format json|jsonl|pretty|toon`, `--error-format text|json`, `--no-color`, `--no-input`, `--debug`.

products are addressed by the `ref` that `search` prints: `GROUP/MODEL` (`303295/8513471`) for decathlon's own products, `mp/UUID/cN` for marketplace sellers. a product url works too.

## examples

```bash
# every filter the site has
decathlon search "hornillo gas" --brand campingaz --max-price 40 --sort price_asc --format pretty
decathlon search --category deportes/camping/tiendas-campana --store 169 --delivery 48h --max-price 60
decathlon search filters --category deportes/camping/tiendas-campana --format pretty
decathlon search --category deportes/camping/tiendas-campana --filter "Nº de plazas=2 plazas"

# stock
decathlon store stock 336529/8668913 --size 41 --store 169 --store Badalona --all --format pretty
decathlon store stock 303295/8513471 --format jsonl | jq -r 'select(.in_stock) | .store_name'

# watches
decathlon watch add product 303295/8513471 --store 169 --name tent --notify phone
decathlon watch add search --category deportes/camping/tiendas-campana --store 169 --max-price 60 --name cheap-tents
decathlon watch check --format jsonl | jq -r 'select(.type=="store.in_stock") | "\(.store.name): \(.product.title)"'
```

event types: `product.new`, `product.price_changed`, `product.removed`, `sku.new`, `sku.back_online`, `sku.sold_out_online`, `store.in_stock`, `store.out_of_stock`.

run checks on a schedule with cron or a systemd timer:

```cron
*/30 * * * * decathlon watch check --format jsonl >> ~/decathlon-events.jsonl 2>> ~/decathlon-watch.log
```

or keep `decathlon watch run --interval 30m` in the foreground.

## sinks config

sinks live in `config.toml` (`decathlon config path`):

```toml
stores = ["169"]                 # default stores for stock and product watches

[location]                       # default centre for distances
lat = 42.8125
lng = -1.6458

[sinks.phone]
type = "ntfy"
url = "https://ntfy.sh"
topic = "my-decathlon-alerts"

[sinks.script]
command = ["/home/me/bin/on-decathlon-event"]   # gets each event as json on stdin
type = "exec"

[sinks.chat]
template = "discord"                            # or "slack"; unset sends the raw event
type = "webhook"
url = "https://discord.com/api/webhooks/..."

```

## updates

```bash
decathlon update --check
decathlon update --yes
```

used at a terminal, the cli looks for a newer stable version no more than once a day, and asks first. scripts, `watch`, `mcp` and `--no-input` are never interrupted; `DECATHLON_NO_UPDATE_CHECK=1` turns the lookup off entirely. a downloaded archive must match `checksums.txt` before it replaces anything, and builds from source (`dev`) are left alone.

## exit codes

| code | when |
|---|---|
| `0` | it worked, including "no changes" |
| `1` | anything else went wrong |
| `2` | bad input: flag, filter, ref, a store filter on a keyword search |
| `4` | no such product, model, size, store or watch |
| `5` | cloudflare refused even a fresh session, or http 429 |
| `6` | decathlon.es or flaresolverr can't be reached |
| `7` | a page no longer has the shape the parser expects; an issue link with the details is printed |
| `130` | ctrl-c |

`3` is kept free for auth, which this cli doesn't do.

## shell completion

```bash
decathlon completion zsh > "${fpath[1]}/_decathlon"
decathlon completion bash > ~/.local/share/bash-completion/completions/decathlon
decathlon completion fish > ~/.config/fish/completions/decathlon.fish
```

## building from source

```bash
git clone https://github.com/n0nuser/decathlon-cli && cd decathlon-cli
make build     # bin/decathlon
make check     # vet, staticcheck, tests, gofmt
```

tests never touch the network. they replay real pages captured from the site against local fakes of the storefront and flaresolverr.

one live smoke test is opt-in. with flaresolverr running and a session already minted (or mintable), it does a real search:

```bash
DECATHLON_LIVE_TEST=1 go test ./internal/cli -run TestLiveSearch -v
```

without the variable it is skipped, so `go test ./...` stays offline.

## documentation

full docs: [n0nuser.github.io/decathlon-cli/docs](https://n0nuser.github.io/decathlon-cli/docs). the command reference there is generated from the binary (`make docs`).

- [examples](examples/README.md): ready-to-run recipes
- [docs/cli-spec.md](docs/cli-spec.md): what every command accepts and prints
- [docs/troubleshooting.md](docs/troubleshooting.md): blocked, solver down, IP blocks
- [docs/decathlon-api-discovery.md](docs/decathlon-api-discovery.md): how the storefront was read
- [docs/adr](docs/adr): decisions
- [AGENTS.md](AGENTS.md): vocabulary and rules for contributors and agents

## contributing

bug reports and pull requests are welcome. if a command exits `7`, the storefront changed under the parser: open the link it prints, which already holds the version, command and page.

## disclaimer

this is an unofficial, independent project. it is not affiliated with, endorsed by or sponsored by decathlon. "decathlon" is used only to say which website the tool works with, and it is a trademark of its owner. the cli reads the same public pages a browser does, for interoperability, and never logs in, buys or changes anything. use it responsibly: it spaces its requests and caches what it can, so keep watch intervals reasonable and don't run it in parallel against the site. you are responsible for following decathlon.es's terms of use.

## license

[mit license](LICENSE)
