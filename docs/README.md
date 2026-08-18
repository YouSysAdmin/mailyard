# Mailyard documentation

A Hugo site. `hugo.toml` explains the settings that are not obvious - this
covers the shape of the directory and the two commands.

```
content/en/            the pages, one directory per section, _index.md is the section
themes/mailyard/       the theme, vendored - layouts, styles, icons, fonts, the mark
dist/                  the build, embedded into the binary by docsite.go
```

There is NO override layer any more, and that is the point: the theme is
OURS, so a change goes where the thing it changes lives rather than into a
second copy that wins on load order. `themes/mailyard` was forked from
`youstsadmin-hugo-docs`, which was a git submodule here - a fresh clone
without it built a site of BLANK pages while Hugo reported success, which
is why three separate places had to test for `theme.toml` first. Upstream
fixes are cherry-picked now, not pulled.

```bash
task docs      # build into dist/ (what the binary embeds)
task docs-dev  # live editing on http://localhost:1313/docs/
```

## Adding a page

Drop a markdown file into its section with `title`, `description` and
`weight` in the front matter. There is NO hand-written sidebar: navigation
is built from the directory tree and ordered by `weight`, so a page cannot
be added and left out of the menu.

A new section is a directory with an `_index.md` carrying `title`,
`description`, `weight` and `icon` - the icon being a file name under
`themes/mailyard/assets/icons`.

Admonitions are the callout shortcode:

```
{{< callout type="warning" title="Optional title" >}}
Body in **markdown**.
{{< /callout >}}
```

Types: `note`, `info`, `tip`, `success`, `warning`, `danger`.

## Links between pages

Written absolute, with the prefix the binary serves under:
`/docs/<section>/<page>`. Nothing checks them - the pages are proofread,
not asserted - so follow one after moving a page.
