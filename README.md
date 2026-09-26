# go-pkgx.github.io

The landing page for [go-pkgx](https://github.com/go-pkgx), built with Hugo and
deployed by GitHub Actions.

It uses the `go-*` family layout: `layouts/partials/styles.html` for the whole
design system, `layouts/partials/theme-toggle.html` for the three-way theme
toggle (system / light / dark, defaulting to system), and a `layouts/index.html`
whose repo cards are drawn from `[[params.repos]]` in `hugo.toml`.

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
