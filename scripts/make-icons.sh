#!/bin/sh
# Regenerates every raster icon from the two committed SVGs.
#
# Run it after changing the mark. Needs rsvg-convert and ImageMagick:
#   brew install librsvg imagemagick
#
# Referenced by nothing on purpose - it is run by hand - and it is the
# only record of how the committed rasters were produced: which SVG
# feeds which output, at what size, and what the flattening is.
#
# It was DELETED once, in e868ea5, while the notes went on describing
# it. What came back had to be rewritten anyway: it still wrote
# docs/static/img/, which the Hugo migration replaced with
# docs/static/assets/logo/, so it would have failed on its third
# command.
set -eu

cd "$(dirname "$0")/.."

SMALL=web/public/favicon.svg              # solid letter, heavier stroke, for <= 20px
FULL=docs/static/assets/logo/logo.svg     # stroked envelope, for larger use

# ONE TONE, and a mid grey rather than either end of the accent.
#
# The SVGs carry a two-colour mark and the small one carries a
# prefers-color-scheme block, so it follows the tab bar. A PNG or an ICO
# can do neither: one file is served to a light tab bar and a dark one
# alike. Near-black would vanish on the second, near-white on the first.
#
# So the raster tone is the same band the docs header logo is pinned to,
# for the same reason - measured, not picked: #818181 is 3.9:1 on white,
# 4.2:1 on a typical dark tab bar and 5.2:1 on the docs' near-black.
# Flattened to a single value because at 16px the two-colour distinction
# was never legible anyway.
FLAT='#818181'
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

flatten() {
  sed -e "s/stroke: #[0-9a-fA-F]\{6\}/stroke: $FLAT/g" \
      -e "s/fill: #[0-9a-fA-F]\{6\}/fill: $FLAT/g" "$1" > "$2"
}

flatten "$SMALL" "$tmp/small.svg"
flatten "$FULL"  "$tmp/full.svg"

# Console. index.html links the SVG first and the PNG as the fallback.
# The ICO is not linked at all and is kept because a browser asks for
# /favicon.ico on its own.
rsvg-convert -w 32 -h 32 "$tmp/small.svg" -o web/public/favicon.png
rsvg-convert -w 16 -h 16 "$tmp/small.svg" -o "$tmp/16.png"
rsvg-convert -w 32 -h 32 "$tmp/small.svg" -o "$tmp/32.png"
rsvg-convert -w 48 -h 48 "$tmp/small.svg" -o "$tmp/48.png"
magick "$tmp/16.png" "$tmp/32.png" "$tmp/48.png" web/public/favicon.ico

# Docs. The theme's head links all four - the SVG, the two PNG sizes and
# the touch icon - out of docs/static/assets/logo/.
rsvg-convert -w 16 -h 16 "$tmp/small.svg" -o docs/static/assets/logo/favicon-16.png
rsvg-convert -w 32 -h 32 "$tmp/small.svg" -o docs/static/assets/logo/favicon-32.png
# 180px is well past the size the small variant exists for, so the touch
# icon takes the full mark. Transparent, like the one it replaces.
rsvg-convert -w 180 -h 180 "$tmp/full.svg" -o docs/static/assets/logo/apple-touch-icon.png

echo "regenerated:"
ls -l web/public/favicon.png web/public/favicon.ico \
      docs/static/assets/logo/favicon-16.png \
      docs/static/assets/logo/favicon-32.png \
      docs/static/assets/logo/apple-touch-icon.png |
  awk '{printf "  %-48s %7s bytes\n", $NF, $5}'
