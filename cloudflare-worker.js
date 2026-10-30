// Route dimetrius-eng.tech/* to this Worker.
const APP_PREFIX = "/gadget-store-crm";
const RENDER_ORIGIN = "https://gadget-store-crm-jj8m.onrender.com";
const RENDER_HOST = new URL(RENDER_ORIGIN).hostname;
const PORTFOLIO_ORIGIN = "https://dimetrius-portfolio.pages.dev";

function addSecurityHeaders(headers) {
  headers.set("Strict-Transport-Security", "max-age=31536000; includeSubDomains");
  headers.set("X-Content-Type-Options", "nosniff");
  headers.set("X-Frame-Options", "DENY");
  headers.set("Referrer-Policy", "strict-origin-when-cross-origin");
  headers.set("Permissions-Policy", "camera=(), microphone=(), geolocation=()");
  headers.set("Cross-Origin-Opener-Policy", "same-origin");
  return headers;
}

export default {
  async fetch(request, env) {
    const incoming = new URL(request.url);
    if (incoming.hostname === "gadget-store-crm.tech" || incoming.hostname === "www.gadget-store-crm.tech") {
      const response = Response.redirect(`https://dimetrius-eng.tech${APP_PREFIX}/overview${incoming.search}`, 301);
      return new Response(null, { status: response.status, headers: addSecurityHeaders(new Headers(response.headers)) });
    }
    if (incoming.pathname === "/robots.txt") {
      return new Response(
        "User-agent: *\nAllow: /\nDisallow: /gadget-store-crm/demo\nDisallow: /gadget-store-crm/login\nDisallow: /gadget-store-crm/products\nDisallow: /gadget-store-crm/sales\nDisallow: /gadget-store-crm/users\nSitemap: https://dimetrius-eng.tech/sitemap.xml\n",
        { headers: { "Content-Type": "text/plain; charset=utf-8", "Cache-Control": "no-store" } },
      );
    }
    if (incoming.pathname === "/sitemap.xml") {
      return new Response(
        '<?xml version="1.0" encoding="UTF-8"?>\n' +
        '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">' +
        '<url><loc>https://dimetrius-eng.tech/</loc></url>' +
        '<url><loc>https://dimetrius-eng.tech/gadget-store-crm/overview</loc></url>' +
        '</urlset>',
        { headers: { "Content-Type": "application/xml; charset=utf-8", "Cache-Control": "no-store" } },
      );
    }
    if (incoming.pathname !== APP_PREFIX && !incoming.pathname.startsWith(`${APP_PREFIX}/`)) {
      const portfolioResponse = await fetch(`${PORTFOLIO_ORIGIN}/`);
      const headers = addSecurityHeaders(new Headers(portfolioResponse.headers));
      headers.set("Cache-Control", "no-store");
      return new Response(request.method === "HEAD" ? null : portfolioResponse.body, {
        status: portfolioResponse.status,
        statusText: portfolioResponse.statusText,
        headers,
      });
    }
    const upstream = new URL(RENDER_ORIGIN);
    upstream.pathname = incoming.pathname.slice(APP_PREFIX.length) || "/";
    upstream.search = incoming.search;

    // Keep redirects visible to the browser so their Location can be rebased
    // under APP_PREFIX instead of being followed internally at the Render URL.
    const requestHeaders = new Headers(request.headers);
    requestHeaders.delete("X-Origin-Verify");
    if (env.ORIGIN_VERIFY_SECRET) requestHeaders.set("X-Origin-Verify", env.ORIGIN_VERIFY_SECRET);
    const requestInit = {
      method: request.method,
      headers: requestHeaders,
      redirect: "manual",
    };
    if (request.method !== "GET" && request.method !== "HEAD") requestInit.body = request.body;
    const upstreamRequest = new Request(upstream, requestInit);
    const originResponse = await fetch(upstreamRequest);
    const headers = addSecurityHeaders(new Headers(originResponse.headers));
    if (upstream.pathname !== "/overview") headers.set("X-Robots-Tag", "noindex, nofollow");

    const location = headers.get("Location");
    if (location) {
      try {
        const redirect = new URL(location, RENDER_ORIGIN);
        if (redirect.hostname === RENDER_HOST || redirect.hostname === incoming.hostname) {
          const path = redirect.pathname.startsWith(APP_PREFIX)
            ? redirect.pathname
            : `${APP_PREFIX}${redirect.pathname}`;
          headers.set("Location", `${path}${redirect.search}${redirect.hash}`);
        }
      } catch (_) {}
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
    // Prefix root-relative app links, assets, forms, and JS fetch/navigation URLs.
    body = body.replace(/(["'])\/(?!\/|gadget-store-crm(?:\/|["']))/g, (_match, quote) => `${quote}${APP_PREFIX}/`);
    headers.delete("Content-Length");
    headers.delete("Content-Encoding");
    headers.delete("ETag");
    return new Response(body, { status: originResponse.status, statusText: originResponse.statusText, headers });
  },
};
