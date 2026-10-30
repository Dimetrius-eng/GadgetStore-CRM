# Security notes and production checklist

Security can be improved substantially, but no website can be guaranteed to be 100% safe. This repository contains the CRM backend and Cloudflare Worker proxy; the separate portfolio Pages source is not present here, so its source code and Pages settings could not be audited.

## Protections in this version

- Employee sessions are stored in PostgreSQL as SHA-256 digests of random 256-bit tokens, with a 20-minute idle timeout and a 12-hour absolute timeout. Logout deletes the server-side session. Deleting a user cascades to that user's sessions.
- Passwords use salted PBKDF2-SHA256 with 600,000 iterations. Older hashes are upgraded after a successful login. Production refuses weak initial passwords, oversized password inputs, a missing origin secret, or a missing/non-TLS PostgreSQL URL. New manager passwords must be at least 12 characters.
- Unsafe browser requests must come from an explicitly allowed HTTPS origin. Logout is a POST, form uploads have request-size limits, and the HTTP server has read/write/idle timeouts and a header-size limit.
- Responses receive browser security headers; CRM HTML and data are marked `no-store`. SQL uses parameters, and Go's `html/template` escapes data rendered into HTML.
- When `SUPABASE_URL` is configured, startup revokes `anon` and `authenticated` access to the PostgreSQL `public` schema (including existing tables/functions and future defaults). This CRM uses direct PostgreSQL from Go, not the Supabase Data API. Do not deploy this change if another app intentionally relies on those public REST grants in the same Supabase project.
- Database backups and Wrangler state are excluded from Git and Docker build context.

## Required before enabling origin protection

The Render origin is otherwise publicly addressable, which lets someone bypass Cloudflare's edge rules. The Worker and Go service use a shared origin-verification secret. The Go service requires it in production; configure the same value in both dashboards before deploying the Go changes. A missing Worker secret or a mismatch will make CRM requests fail.

1. Generate a fresh secret locally, for example with `node -e "console.log(require('crypto').randomBytes(32).toString('hex'))"`. Do not paste it into source files, GitHub, chat, or a browser.
2. Add that exact value as the **secret** `ORIGIN_VERIFY_SECRET` in the Cloudflare Worker settings for the Worker that serves `dimetrius-eng.tech`.
3. Add the same value as the `ORIGIN_VERIFY_SECRET` environment variable in the Render `gadget-store-crm` service. Keep it private.
4. Deploy the updated Worker and Render service only after both settings are present. The Worker overwrites any visitor-supplied `X-Origin-Verify` header with its secret; the Go service rejects direct requests to Render without it. The Go service now fails startup in production if the origin secret is missing or shorter than 32 characters.
5. Confirm the CRM works through `https://dimetrius-eng.tech/gadget-store-crm/login`, then confirm the direct `https://gadget-store-crm-jj8m.onrender.com/login` returns `403 Forbidden`.

Render's direct health check must use `/healthz`, which returns no application data and is intentionally exempt from origin-secret verification. `render.yaml` sets this path; if the Render service is managed from the dashboard rather than a Blueprint, update its Health Check Path to `/healthz` there before deploying the Go changes.

Until all five steps are complete, direct Render-origin access is still possible. Do not deploy the Go changes before the Worker secret and Worker code are ready.

## Cloudflare and Supabase dashboard checks

- On the `dimetrius-eng.tech` zone, create a rate-limiting rule for `/gadget-store-crm/login`, counted by source IP. The Free plan supports one rate-limiting rule; use a modest threshold (for example, 10 requests per 10 seconds) and a short mitigation period to reduce password guessing without locking out shared networks. Cloudflare documents the current plan limits and setup in [Rate limiting rules](https://developers.cloudflare.com/waf/rate-limiting-rules/) and [creating a dashboard rule](https://developers.cloudflare.com/waf/rate-limiting-rules/create-zone-dashboard/).
- Keep `DATABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY`, `ADMIN_PASSWORD`, and `MANAGER_PASSWORD` only in Render's secret environment settings. Rotate any credential that has ever been committed, pasted into a public issue, or shared outside the trusted operator group.
- The application uses Supabase's elevated service role only from Go for Storage operations. Supabase states that this key bypasses RLS; never put it in browser code. If the database or Storage Data API is enabled for public clients, verify least-privilege grants and RLS policies independently. A public `product-images` bucket makes image URLs public by design; do not store private documents there. See [Supabase secure data](https://supabase.com/docs/guides/database/secure-data) and [Storage access control](https://supabase.com/docs/guides/storage/security/access-control).
- Confirm Supabase automated backups/exports are available for the data's importance, and periodically test restoring a backup. A free hosting/database plan is not a disaster-recovery guarantee.
- Review the separate portfolio Pages project, its GitHub repository permissions, Cloudflare Pages deployment settings, and any third-party scripts separately. Keep its public content free of secrets and private customer data.

## Scope and remaining limitations

- The demo is intentionally public and accepts edits/uploads. Session data is isolated by a random cookie and expires, but public demo traffic can still consume shared database/storage quotas. Cloudflare rate limiting and Supabase usage alerts are important.
- Employee authorization state is database-backed, but availability still depends on Render and Supabase. A compromised Render/Cloudflare/Supabase account or leaked database/service key is outside the protection provided by application code. Enable MFA on those operator accounts and use unique passwords.
- The app still relies on third-party CDN CSS/fonts and inline scripts/styles. This update does not add a strict Content Security Policy because that could break the current UI; a CSP requires inventorying and migrating those resources first.
- This checklist is not a penetration test or a guarantee. After enabling the protections, review Cloudflare security events, Render logs, Supabase audit/logging, and account MFA regularly.
