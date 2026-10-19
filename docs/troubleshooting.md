# troubleshooting

Most failures are the Cloudflare session or the local FlareSolverr. Start with these two:

```bash
decathlon doctor --format pretty     # what is broken, check by check
decathlon doctor --mint --debug      # throw the session away, mint a new one, log every request
```

`doctor` exits 1 if any check fails and works through these, in order:

- `config`: `config.toml` parses (a broken file is reported here, not as a usage error).
- `sink NAME`: each notification target in config is valid.
- `flaresolverr`: the solver answers, with its version.
- `session`: the stored `session.json` exists (minted at, how many cookies), or a new one was minted.
- `decathlon.es`: the home page really comes back over `net/http` with that session.

`--mint` discards the stored session first, so the `session` line tells you whether FlareSolverr
can mint at all. `--debug` prints one line per request to stderr, with status, size and timing.

There is no separate session command: the session is managed for you. `decathlon config path`
prints where `config.toml`, `session.json` and the watch database live. In scripts, add
`--error-format json` to get `{code, category, retryable, message, …}` on stderr; exit 5 means
Cloudflare refused, 6 means the site or FlareSolverr is unreachable, 7 means a page changed shape.

## "blocked" or a cloudflare challenge (`Attention Required`, `challenges.cloudflare.com`)

The interstitial ("Just a moment…", `window._cf_chl_opt`, a `Cf-Mitigated: challenge` header)
means the cookies FlareSolverr minted have gone stale — they last about 30 minutes idle — or
Cloudflare rotated its view of this client.

The CLI does not give up on the first one: it mints once and retries once, and says so on stderr:

```text
decathlon: cloudflare challenged the stored session; minting a new one through flaresolverr
```

If the retry is challenged too, the command exits 5 with `cloudflare challenged the request` and
stops there. It never loops. Restart the solver, then force a fresh session:

```bash
docker restart flaresolverr
decathlon doctor --mint
```

When `doctor` shows the home page fetched with the new session, the next commands work again.

## `flaresolverr is not reachable` / `Start it (see README)`

The solver is not running, so no session can be minted. Nothing is wrong with the site:

```text
decathlon: flaresolverr is not reachable at http://localhost:8191 (dial tcp …). Start it (see README) or set FLARESOLVERR_URL, then run `decathlon doctor`
```

Exit code 6. Start it, or restart the container you already have:

```bash
docker run -d --name flaresolverr -p 127.0.0.1:8191:8191 --restart unless-stopped \
  ghcr.io/flaresolverr/flaresolverr:latest
docker start flaresolverr   # if the container already exists
```

It listens on `http://localhost:8191`. Point the CLI somewhere else with the environment variable,
or save it in config (`FLARESOLVERR_URL` wins over `config.toml`):

```bash
FLARESOLVERR_URL=http://192.168.1.10:8191 decathlon search "tienda campaña"
decathlon config set flaresolverr_url http://192.168.1.10:8191
```

Check the solver on its own with `curl -s http://localhost:8191/`, which answers
`{"msg": "FlareSolverr is ready!", …}`. If something else owns that port, the CLI reports
`<url> does not look like flaresolverr`. Tested with FlareSolverr 3.5.2.

## "access restricted" / cloudflare blocking this address

A different failure: FlareSolverr's own browser is blocked too, so `--mint` cannot help. `doctor`
shows the `session` check failing with `flaresolverr could not open decathlon.es: …`, and ordinary
commands exit 5. That is IP reputation, not a stale session. It usually lifts within an hour or
a few hours.

- Stop all traffic from this network, watches included: `decathlon watch check` and
  `decathlon watch run` read the same pages.
- Wait, then retry once with `decathlon doctor`.
- Keep the rate low afterwards: `decathlon watch add … --interval 1h` (the floor is 5 minutes,
  the default 30), and do not run `watch check` from cron while also using the CLI by hand.
- A VPN, a shared address or a cloud host makes it likelier. Try another network if you have one.

Hammering the site through a block makes it last longer.

## do I need an account or to log in?

No. There is no account, no login, and nothing to export. The CLI asks FlareSolverr to open
decathlon.es once in a real browser, then keeps the cookies and user agent that browser was
given in `session.json` (mode 0600) and replays them with plain `net/http` — which is why later
requests are fast. `decathlon config path` shows the file; `decathlon doctor --mint` replaces it.

Nothing here adds to a cart, checks out, or touches an account
([ADR 0003](adr/0003-read-only.md)).

## will this get me banned, or is it allowed?

It is a small, read-only personal tool. Every request is a GET of a page a browser would load,
at least 2 seconds apart, with the store directory and category menu cached. It never logs in,
buys, posts, or crawls a catalogue.

decathlon.es's robots.txt exempts only `/robots.txt` from the challenge and disallows the
account, checkout and `/api/` areas, among other patterns; the CLI stays out of those and has no
cart or account features. Keep it polite anyway: watch intervals are at least 5 minutes (30 by
default), do not run it in parallel, and do not point it at page after page. You are responsible
for following decathlon.es's terms of use.

## still failing

Rerun the failing command with `--debug` and read the `GET …` lines: they show each URL, status,
size and time. `--error-format json` gives the same failure as an envelope, and exit 7 prints a
prefilled issue link for when a page no longer has the shape the parser expects. What each page
is expected to contain is in [decathlon-api-discovery.md](decathlon-api-discovery.md).
