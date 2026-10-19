# 0001. FlareSolverr mints the Cloudflare session; Go net/http does the reading

Date: 2026-10-19
Status: accepted

## Context

Cloudflare serves a managed challenge ("Just a moment…", `cf-mitigated: challenge`) to every
storefront path on www.decathlon.es for clients without a challenge cookie. Curl and Go
`net/http` are both challenged, even with a full browser header set. See
`docs/reference-decathlon-feasibility.md`.

The user runs FlareSolverr locally (v3.5.2, default `http://localhost:8191`) and allowed the
CLI to use it. They first asked for every request to go through it. They then allowed minting
a session there and replaying it from Go, as long as Go's own TLS was checked first.

## The check (2026-10-19)

A single FlareSolverr `request.get` of `/es/` returns:
- cookies `__cf_bm` (30-minute `Expires`), `_cfuvid`, `AUTH_STATE`, `didomi_token` and `dkt_ecom_user_temporary_id`, but no `cf_clearance`;
- the user agent of its browser.

Go 1.26 `net/http`, with stdlib TLS and HTTP/2, sending those cookies under that exact user agent:

| request | without cookies | with cookies |
|---|---|---|
| `/es/search?Ntt=hornillo+gas` | 403 challenge | 200 |
| `/es/p/x/303295/m8513471` (redirect) | 403 challenge | 200 |
| `/es/deportes/camping/tiendas-campana/f-store_pamplona?price=from_10_to_40` | 403 challenge | 200 |

Every fixture in `internal/api/testdata` was captured this way: one mint, then eight plain Go
requests, all 200, with a real 404 for an unknown product. Go TLS passes, so FlareSolverr is not
needed for each request.

## Decision

- `internal/api.Client` loads the session from `session.json` (cookies + user agent, mode 0600, atomic write) in the config dir. When there is none, it mints one through FlareSolverr's `/v1` `request.get`.
- Every storefront request is a plain Go GET carrying that user agent and a cookie jar. Cookies that Cloudflare rotates (`__cf_bm`) are written back.
- On a challenge (403/503 with the Cloudflare page, or `cf-mitigated: challenge`), the client mints once and retries once. A second refusal exits 5. It never loops.
- A 429 exits 5 without re-minting.
- If FlareSolverr is unreachable, the command exits 6 with a hint to start it and run `decathlon doctor`.
- `FLARESOLVERR_URL` (env) or config `flaresolverr_url` select the instance.
- Requests are sequential, with at least 2 s between them.
- The store directory and the category menu are cached for 24 h. The unfiltered page used to resolve filter values is cached for 10 min. Search results and product pages are never cached, so watches see fresh data.

## Dependencies

The transport uses the stdlib only (`net/http`, `crypto/tls`, `net/http/cookiejar`). There is no
TLS-fingerprint client and no browser in the binary. The rest of the CLI uses cobra/pflag
(commands, completion, generated reference), `pelletier/go-toml/v2` (config.toml), pure-Go
`modernc.org/sqlite` (watch state, so the binary stays static), `adrg/xdg` (paths) and
`toon-format/toon-go` (`--format toon`).

## Consequences

- FlareSolverr must be running for the first request and whenever the session goes stale (about every 30 min of idleness). Tests never mint: they parse fixtures.
- If Cloudflare starts binding the cookie to the TLS fingerprint, the re-mint fails twice and commands exit 5. The fallback then is to send every request through FlareSolverr, the user's original design, which only changes the transport.
