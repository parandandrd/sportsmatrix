# SportsMatrix web UI

The React app the Go binary embeds and serves. It is built with
[Vite](https://vite.dev) and tested with [Vitest](https://vitest.dev).

```bash
npm ci --legacy-peer-deps   # see CLAUDE.md for why the flag
npm test                    # vitest, once; `npx vitest` watches
npm run build               # into build/, which script/build embeds
npm start                   # dev server
```

The app talks to the Twirp services on whatever host served it, so pages from
the dev server have no backend behind them. To try a change against a real
instance, build it and run the Go binary, e.g. `sportsmatrix run --test` with a
config whose `httpListenPort` is free.

`build.assetsDir` is `static` on purpose: the Go side serves everything under
`static/` with a year-long immutable cache, which is only safe because Vite
names those files after their content.
