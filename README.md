# go-pkgx.github.io

The landing page for [go-pkgx](https://github.com/go-pkgx), built with Hugo and
deployed by GitHub Actions.

It uses the `go-*` family layout: `layouts/partials/styles.html` for the whole
design system, `layouts/partials/theme-toggle.html` for the three-way theme
toggle (system / light / dark, defaulting to system), and a `layouts/index.html`
whose repo cards are drawn from `[[params.repos]]` in `hugo.toml`.

## The playground, at `/playground/`

`content/playground.md` + `layouts/_default/playground.html` + `static/playground/`
run the factory's `wasip1/wasm` bottles **in the reader's browser**: `wasi.js` is a
WASI preview1 host, `presets.js` holds one proven command line per module, and
`app.js` is the page.

Three things about it are load-bearing:

- **It never fetches a registry.** `ghcr.io` sends no CORS headers, so the modules are
  staged into `static/playground/modules/` during the build by `tools/wasmstage`, and
  the page reads `index.json` from this site. **No `.wasm` is committed**; `.gitignore`
  keeps the staged tree out.
- **A syscall the host does not implement returns `ENOSYS` and names itself on the
  page.** Never 0: a run that silently skipped a call and exited cleanly is
  indistinguishable from one that worked.
- **It is confronted with `wasmtime`, not asserted.** `tools/playground` runs all eight
  modules under the reference implementation, records stdout, stderr and the exit status
  in `testdata/fixtures.json`, then runs the same modules through `static/playground/wasi.js`
  itself and requires the bytes to match. Running it:

  ```sh
  cd tools/wasmstage && go run . -out /tmp/staged \
      zlib.net lua.org sqlite.org filippo.io/age mvdan.cc/sh \
      github.com/cpuguy83/go-md2man github.com/simdjson/simdjson
  cd ../playground && PLAYGROUND_WASM_DIR=/tmp/staged go test ./...   # add -update to re-record
  ```

  Without `PLAYGROUND_WASM_DIR`, `wasmtime` or `node`, those tests **skip** — and a skip
  is not a pass. The one difference the confrontation has caught so far: sqlite3 printed a
  233-byte box-drawing table where wasmtime printed `3.53.4`, because `fd_fdstat_get`
  reported stdout as a terminal. `isatty()` is literally those bits.

## The stylesheet takes its colours as parameters

`go-authn` and `go-fileshare` carry byte-identical copies of this stylesheet
with cyan written into it. That is *their* colour: both their logos are the
cyan gradient `#22CCE2 → #0079A8`. go-pkgx's logo is amber, `#F59E0B →
#B45309`, so a third copy would have differed from the other two in one block
of colours — which is how a shared stylesheet stops being shared.

So the brand colours come from `[params.brand]`, and **every default is the
cyan the other two use today**. The other two can adopt this file with no
params at all and render exactly what they render now.

That is checked rather than claimed:

```sh
# build go-authn as it ships, and this site's stylesheet with no brand params
hugo --destination /tmp/ref   --source ../go-authn.github.io
hugo --destination /tmp/ours  # with [params.brand] commented out
# then compare the emitted <style> blocks — they are identical
```

Two things that check caught, and that reading the file would not have:

- **`ZgotmplZ`.** Go's `html/template` refuses to interpolate `rgba(...)` into
  a CSS context and substitutes that marker for the whole declaration. It does
  not fail; it renders nonsense. The chip tints came out as
  `--chip-bg:ZgotmplZ` until `safeCSS` was applied — to *every* value, including
  the hex ones that do not need it, so nobody has to work out which kind each
  value is.
- **Comparing against the wrong thing.** The first check diffed the render
  against the reference *source file* and reported a difference that was not
  one: `html/template` strips CSS and JS comments from inline `<style>` and
  `<script>`, so the source and the render never match. The control has to be
  render against render.

## Numbers on the page are derived

`{{ len .Site.Params.repos }}`, not a digit typed next to the list it counts.
The sibling orgs learned this the hard way — a page said "Nine repositories"
above a list of ten, because a repository was added and the sentence above it
was not, and nothing fails when that happens.

Figures that are measurements rather than counts (binary size, the pantry
sweep) are prose, and each is stated where it was measured.
