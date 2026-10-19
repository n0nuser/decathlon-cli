# 0003. Read-only: no cart, checkout or account

Date: 2026-10-19
Status: accepted

## Context

The point of the CLI is finding gear and which store has it in stock, before going to buy it.
Every piece of that data is public. Carts and checkout need an account session, CSRF handling
and payment flows, and their mistakes cost money.

## Decision

- The CLI only GETs public pages.
- It has no login, cart, wishlist or checkout command, and the MCP server exposes only read tools and watch checks.
- The one session it holds is the Cloudflare cookie set (ADR 0001), which identifies no one.
- `product open` hands off to the browser for buying.

## Consequences

- Nothing in the CLI can spend money or change an account.
- Exit code 3 (auth) stays reserved and unused.
- If store stock ever needs an account (it does not today), that is a new ADR, not a quiet addition.
