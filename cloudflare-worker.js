// Route dimetrius-eng.tech/* to this Worker.
const APP_PREFIX = "/gadget-store-crm";
const RENDER_ORIGIN = "https://gadget-store-crm-jj8m.onrender.com";
const RENDER_HOST = new URL(RENDER_ORIGIN).hostname;
const PORTFOLIO_PAGE = `<!doctype html>
<html lang="uk"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="theme-color" content="#f7f6f2"><meta name="description" content="Dimetrius Engine — персональний сайт і добірка проєктів."><link rel="canonical" href="https://dimetrius-eng.tech/"><title>Dimetrius Engine — розробник і проєкти</title><style>
:root{font-family:Inter,ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;color:#202522;background:#f7f6f2}*{box-sizing:border-box}body{margin:0}.wrap{width:min(1060px,calc(100% - 44px));margin:auto}a{color:inherit;text-decoration:none}.top{height:82px;display:flex;align-items:center;justify-content:space-between;border-bottom:1px solid #dedfd9}.brand{font-size:17px;font-weight:760;letter-spacing:-.035em}.top small{color:#72786f;font-size:13px}main{padding:90px 0 105px}.intro{max-width:720px}.kicker{color:#657363;font-size:12px;text-transform:uppercase;letter-spacing:.15em;font-weight:740}.intro h1{font-size:clamp(44px,8vw,82px);line-height:.99;letter-spacing:-.07em;margin:22px 0}.intro h1 span{color:#55705d}.intro p{max-width:610px;color:#646b64;font-size:18px;line-height:1.75;margin:0}.section-head{display:flex;align-items:end;justify-content:space-between;margin:86px 0 18px}.section-head h2{font-size:24px;letter-spacing:-.04em;margin:0}.section-head span{color:#7e847d;font-size:13px}.projects{display:grid;grid-template-columns:repeat(2,1fr);gap:16px}.project{min-height:220px;border:1px solid #dedfd9;border-radius:18px;padding:26px;background:#fff}.project-label{font-size:12px;color:#70796e}.project h3{font-size:23px;letter-spacing:-.04em;margin:24px 0 9px}.project p{max-width:520px;margin:0;color:#646d64;line-height:1.65;font-size:14px}.project a{display:inline-flex;align-items:center;gap:8px;margin-top:22px;font-size:14px;font-weight:700;color:#345442}.project.secondary{display:flex;flex-direction:column;justify-content:center;background:transparent}.project.secondary h3{font-size:18px;margin:10px 0}.project.secondary p{font-size:13px}.contact{display:flex;align-items:center;justify-content:space-between;gap:18px;margin-top:56px;padding-top:22px;border-top:1px solid #dedfd9;color:#687068;font-size:14px}.contact a{text-decoration:underline;text-underline-offset:4px}footer{border-top:1px solid #dedfd9;padding:22px 0;color:#858b83;font-size:12px}@media(max-width:650px){.wrap{width:calc(100% - 32px)}.top{height:70px}main{padding:62px 0 75px}.intro p{font-size:16px}.section-head{margin-top:62px}.projects{grid-template-columns:1fr}.project{min-height:190px;padding:21px}.contact{align-items:flex-start;flex-direction:column}}
</style></head><body><header class="top wrap"><a class="brand" href="/">Dimetrius Engine</a><small>Портфоліо · проєкти</small></header><main class="wrap"><section class="intro"><div class="kicker">Вітаю на моєму сайті</div><h1>Привіт, я<br><span>Дмитро Руденко.</span></h1><p>Тут я збираю власні проєкти та рішення, над якими працюю. Кожен із них починався з практичної задачі й розвивається крок за кроком.</p></section><section><div class="section-head"><h2>Вибрані проєкти</h2><span>01 / Проєкти</span></div><div class="projects"><article class="project primary"><div class="project-label">Облік та керування магазином</div><h3>GadgetStore CRM</h3><p>Вебсистема для товарного обліку, залишків і продажів у магазині електроніки. Можна переглянути демонстрацію або відкрити сторінку проєкту.</p><a href="/gadget-store-crm/overview">Переглянути проєкт <span aria-hidden="true">↗</span></a></article><article class="project secondary"><div class="project-label">Розділ поповнюється</div><h3>Інші проєкти — згодом</h3><p>Тут з’являться нові роботи та експерименти.</p></article></div></section><section class="contact"><span>Є запитання або пропозиція?</span><a href="https://mail.google.com/mail/?view=cm&amp;fs=1&amp;to=mydimka08%40gmail.com" target="_blank" rel="noopener noreferrer">Написати мені · mydimka08@gmail.com</a></section></main><footer><div class="wrap">© 2026 Dimetrius Engine</div></footer></body></html>`;

export default {
  async fetch(request) {
    const incoming = new URL(request.url);
    if (incoming.hostname === "gadget-store-crm.tech" || incoming.hostname === "www.gadget-store-crm.tech") {
      return Response.redirect(`https://dimetrius-eng.tech${APP_PREFIX}/overview${incoming.search}`, 301);
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
      return new Response(PORTFOLIO_PAGE, {
        headers: {
          "Content-Type": "text/html; charset=utf-8",
          "Cache-Control": "no-store",
        },
      });
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
