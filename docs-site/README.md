# docs-site

The documentation site for `decathlon-cli`, built with Fumadocs on Next.js and exported
as static files. The site is served from a placeholder domain for now (`https://example.com`).

Search runs in the browser against the index emitted at `/api/search`, and `/llms.txt`
is emitted during the build, so the exported site needs no running server.

```bash
npm install
npm run dev      # http://localhost:3000/docs
npm run typecheck
npm run build    # writes the static export to out/
npm start        # serve out/ at http://localhost:3000
```

`npm install` runs `fumadocs-mdx` through `postinstall` to generate the content types
before typechecking or building. `npm start` needs Python 3.

## Command reference

`content/docs/reference/*.mdx` is generated from the CLI's cobra tree by
`cmd/docsgen`, one page per top-level command. Do not edit the generated pages by
hand: change the command's `Short`, `Long` or flag usage strings in Go, then
regenerate.

```bash
go run ./cmd/docsgen
```

`content/docs/reference/index.mdx` is written by hand and the generator leaves it alone.

## Theme

The palette is a small blue brand scale (`--brand-50` through `--brand-950`) defined in
`app/global.css`. Light mode is a near-white page with a deep-blue primary; dark mode is
a near-black ground with a lighter blue primary. The site defaults to dark.
