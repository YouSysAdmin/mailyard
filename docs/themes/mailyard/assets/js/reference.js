// API reference: mount Scalar on the reference pages and keep its theme in
// step with the site's. The vendored standalone bundle (static/vendor/scalar)
// defines window.Scalar. Both scripts are deferred, and deferred scripts run
// in DOCUMENT order - this bundle sits in <head>, Scalar's in the article -
// so mounting waits for DOMContentLoaded, by which point every deferred
// script has run.
document.addEventListener('DOMContentLoaded', function () {
  var host = document.getElementById('openapi-reference');
  if (!host || !window.Scalar || !host.dataset.url) return;

  var root = document.documentElement;
  function dark() { return root.getAttribute('data-theme') !== 'light'; }

  // Try-it sends ONLY what the reader put in the Authorization field.
  // The reference is same-origin with the console, so a plain fetch
  // would carry the session cookie, and every request then succeeded
  // with the field empty - and a key scoped to one permission answered
  // with the session's whole set, because the server reads a bearer
  // first and the cookie only when there is none. An answer here has to
  // be the key's own, so cookies are dropped from the client's requests.
  // The document itself is still fetched with the session: it sits
  // behind the same gate as this page.
  var specURL = host.dataset.url;
  function fetchAsClient(input, init) {
    var url = typeof input === 'string' ? input : input.url;
    if (url === specURL || url.indexOf(specURL) !== -1) return fetch(input, init);
    return fetch(input, Object.assign({}, init, { credentials: 'omit' }));
  }

  // FOUR OF THESE ARE LOAD-BEARING UNDER THE SITE'S CSP, not preferences:
  // withDefaultFonts pulls from fonts.scalar.com (font-src is 'self'),
  // proxyUrl routes Try-it through proxy.scalar.com, the agent searches
  // api.scalar.com (connect-src is 'self') and telemetry reaches out on
  // its own. All off, so the only request the viewer makes is the
  // document, same-origin, and Try-it goes straight to this instance.
  // showDeveloperTools defaults to "on localhost", which is where a
  // developer would otherwise see a toolbar production never shows.
  var config = {
    url: specURL,
    customFetch: fetchAsClient,
    proxyUrl: '',
    telemetry: false,
    withDefaultFonts: false,
    agent: { disabled: true },
    mcp: { disabled: true },
    showDeveloperTools: 'never',
    hideDarkModeToggle: true,
    hideClientButton: true,
    darkMode: dark(),
    layout: 'modern',
  };
  var app = window.Scalar.createApiReference(host, config);

  // The site's toggle flips data-theme on <html>. Scalar keeps its own
  // notion, so mirror it rather than let the two drift apart. The WHOLE
  // configuration goes back each time: updateConfiguration replaces
  // rather than merges, so a call carrying only darkMode drops the url
  // and every switch above.
  new MutationObserver(function () {
    if (app && typeof app.updateConfiguration === 'function') {
      config.darkMode = dark();
      app.updateConfiguration(config);
    }
  }).observe(root, { attributes: true, attributeFilter: ['data-theme'] });
});
