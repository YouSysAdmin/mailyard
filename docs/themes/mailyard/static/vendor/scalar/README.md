# Scalar API Reference, vendored

`standalone.js` is `@scalar/api-reference@1.67.0`, file
`dist/browser/standalone.js`, unmodified. `LICENSE` is the MIT notice
from github.com/scalar/scalar.

Vendored rather than loaded from a CDN because the documentation is served
from inside the binary under a `script-src 'self'` policy, and because a
plain clone has to build the site - the same reason the theme itself is
vendored and Fuse.js sits in `assets/js/vendor`.

It lives under `static/`, not `assets/`: 3.7 MB through the minify pipe
buys nothing and costs every build.

To update:

    V=1.67.0
    curl -sfL -o standalone.js "https://cdn.jsdelivr.net/npm/@scalar/api-reference@$V/dist/browser/standalone.js"
    curl -sfL -o LICENSE https://raw.githubusercontent.com/scalar/scalar/main/LICENSE

then change the version above and in this file, rebuild, and open
`/docs/reference/api` with the browser console showing - the switches in
`assets/js/reference.js` (`withDefaultFonts`, `proxyUrl`, `telemetry`,
`agent.disabled`) are what keep it inside the CSP, and a new release may
add another thing that reaches out. Also keep the host element's id away
from `api-reference`: the bundle self-mounts on that id with defaults.
