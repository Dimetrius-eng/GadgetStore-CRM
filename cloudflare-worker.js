// Route only dimetrius-eng.tech/gadget-store-crm* to this Worker.
// The domain root continues to be served by GitHub Pages.
const APP_PREFIX = "/gadget-store-crm";
const RENDER_ORIGIN = "https://gadget-store-crm-jj8m.onrender.com";
const RENDER_HOST = new URL(RENDER_ORIGIN).hostname;

function withAppPrefix(path) {
  if (!path || path.startsWith("//")) return path;
  if (path === APP_PREFIX || path.startsWith(`${APP_PREFIX}/`)) return path;
  return `${APP_PREFIX}${path.startsWith("/") ? path : `/${path}`}`;
}

export default {
  async fetch(request) {
    const incoming = new URL(request.url);
    if (incoming.pathname !== APP_PREFIX && !incoming.pathname.startsWith(`${APP_PREFIX}/`)) {
      // If the route was accidentally configured too broadly (for example,
      // dimetrius-eng.tech/*), leave the portfolio and its 404 pages at the
      // DNS origin (GitHub Pages) instead of sending them to the CRM on Render.
      return fetch(request);
    }

    const upstream = new URL(RENDER_ORIGIN);
    upstream.pathname = incoming.pathname.slice(APP_PREFIX.length) || "/";
    upstream.search = incoming.search;

    // Keep redirects visible to the browser so their Location can be rebased
    // under APP_PREFIX instead of being followed internally at the Render URL.
    const requestInit = {
      method: request.method,
      headers: request.headers,
      redirect: "manual",
    };
    if (request.method !== "GET" && request.method !== "HEAD") requestInit.body = request.body;
    const upstreamRequest = new Request(upstream, requestInit);
    const originResponse = await fetch(upstreamRequest);
    const headers = new Headers(originResponse.headers);

    const location = headers.get("Location");
    if (location) {
      try {
        const redirect = new URL(location, RENDER_ORIGIN);
        // Go may generate an absolute Location using either the Render host or
        // the public host forwarded by Cloudflare. Rebase both into the app path.
        if (redirect.hostname === RENDER_HOST || redirect.hostname === incoming.hostname) {
          redirect.pathname = withAppPrefix(redirect.pathname);
          headers.set("Location", `${redirect.pathname}${redirect.search}${redirect.hash}`);
        }
      } catch (_) {
        // Leave non-URL Location values untouched.
      }
    }

    const cookies = headers.getSetCookie?.() || [];
    if (cookies.length) {
      headers.delete("Set-Cookie");
      for (const cookie of cookies) {
        headers.append("Set-Cookie", cookie.replace(/;\s*Path=\/;?/i, `; Path=${APP_PREFIX};`));
      }
    }

    const contentType = headers.get("Content-Type") || "";
    if (!/(text\/html|javascript|manifest\+json|application\/json)/i.test(contentType)) {
      return new Response(originResponse.body, { status: originResponse.status, statusText: originResponse.statusText, headers });
    }

    let body = await originResponse.text();
    // Prefix root-relative links, form actions, asset paths, and JS fetch/navigation
    // URLs while leaving external URLs and already-prefixed URLs unchanged.
    body = body.replace(/(["'])\/(?!\/|gadget-store-crm(?:\/|["']))/g, (_match, quote) => `${quote}${APP_PREFIX}/`);
    headers.delete("Content-Length");
    headers.delete("Content-Encoding");
    headers.delete("ETag");
    return new Response(body, { status: originResponse.status, statusText: originResponse.statusText, headers });
  },
};
