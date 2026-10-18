# decathlon.es feasibility

**Verdict: blocked: Cloudflare managed challenge + TLS-fingerprint gating on every storefront path; only a TLS-impersonating client gets through.**

Probed 2026-10-18 from Spain (Cloudflare POP `MAD`).

## Evidence

| client | `/es/` | `/es/search?Ntt=tienda` | `/robots.txt` |
|---|---|---|---|
| curl 8.x, Chrome/Firefox UA, `Accept-Language: es-ES` | 403 `cf-mitigated: challenge` | 403 challenge | 200 |
| curl `--http1.1`, Googlebot UA, app-like UA | 403 challenge | — | — |
| curl + full Chrome header set (`sec-ch-ua*`, `sec-fetch-*`, `upgrade-insecure-requests`, full `accept`/`accept-encoding`) | 403 challenge | — | — |
| Go 1.26 `net/http`, Chrome UA | 403 `cf-mitigated: challenge` | 403 challenge | — |
| `curl_cffi` `impersonate="chrome"` / `"safari"` / `"firefox"` | **200**, 1.0 MB Next.js HTML | **200**, ~980 KB | — |

The 403 body is Cloudflare's "Just a moment… Enable JavaScript and cookies to continue"
interstitial (`window._cf_chl_opt`, `challenges.cloudflare.com` in CSP). It is served on
every path tried, including `/favicon.ico`, `/manifest.json`, `/sitemap.xml`,
`/_next/static/...`, `/api/*` and product/store-locator URLs. Only `/robots.txt` is exempt.

Curl with the complete Chrome header set is still challenged, while the same request passes
with an impersonated TLS/HTTP2 fingerprint. So the gate is the JA3/JA4 + HTTP/2 fingerprint,
not IP reputation or headers.

`api.decathlon.net` (candidate mobile/partner API host) returns Cloudflare
"Attention Required!" 403 to every client, impersonated included.

## Repro

```sh
curl -s -o /dev/null -w '%{http_code}\n' -D - -A 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36' \
  https://www.decathlon.es/es/ | grep -iE '^(HTTP|cf-mitigated)'
# HTTP/2 403 / cf-mitigated: challenge

python3 -m venv v && v/bin/pip -q install curl_cffi
v/bin/python -c 'from curl_cffi import requests as r; print(r.get("https://www.decathlon.es/es/", impersonate="chrome").status_code)'
# 200
```

## Other hosts (plain curl, no impersonation)

| host | result |
|---|---|
| `api.shoppingapp.decathlon.com`, `navigate.`, `cached-api.`, `internal.`, `checkout.` (mobile-app BFF) | 403 `{"message":"Forbidden"}`, `x-amzn-errortype: ForbiddenException`: AWS API Gateway behind Cloudflare, **no CF challenge**. Needs the app's `x-api-key` (a bogus key is rejected the same way). |
| `engage.shoppingapp.decathlon.com` | 403 `MissingAuthenticationTokenException` (IAM/route-level auth) |
| `api.decathlon.net`, `api-eu.`, `api-global.` | 403 Cloudflare "Attention Required!" to every client |
| `vdp.`, `booking.`, `secondlife-apps.decathlon.net` | 200, but these are side apps (vuln disclosure, workshop booking, buyback), not catalog/stock |

The storefront HTML (fetched via impersonation, for research only) references no client-side catalog/stock API on another host. Its data is server-rendered by Next.js on `www.decathlon.es` itself, which is gated.

Candidate hosts came from public YesWeHack scope enumerations. The mobile BFF is understood to expect `Accept: application/vnd.dkt.shoppingapp.v2+json` and a host-scoped `x-api-key`; neither was verified here.

## Existing landscape

- GitHub search `decathlon api` turns up toy wrappers and Python scrapers (e.g. `Dragon17warrior01/decathlon-api`, 2026). None is a maintained client for the 2026 storefront or BFF.
- Decathlon runs a YesWeHack bug bounty (it appears in `arkadiyt/bounty-targets-data`). Any further work should stay at a sane rate and read-only.

## Backend (from the impersonated fetch)

Next.js App Router storefront (`vary: rsc, next-router-state-tree`, `dkt-ecom-origin: web-navigate-front`),
images on `contents.mediadecathlon.com`. Not inspected further: the gate stops here.

## Workarounds (not chosen)

1. Go TLS-fingerprint client (`bogdanfinn/tls-client` or `refraction-networking/utls` + HTTP/2 frame tuning). Passes today, but it's an arms race with Cloudflare and adds a heavy dependency.
2. Headless browser (chromedp/playwright) to solve the challenge and reuse `cf_clearance`. Heavy, not a single static binary.
3. Mobile-app BFF (`*.shoppingapp.decathlon.com`). Not challenged, so plain Go `net/http` would work, but it needs the `x-api-key` (and possibly a guest token) from the official Android app. That means extracting the APK or MITM-ing the app on a rooted emulator. Lightest runtime path, heaviest discovery; the key can be rotated.
