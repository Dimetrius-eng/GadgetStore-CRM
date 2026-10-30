package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const maxPasswordBytes = 1024

// securityHeaders adds browser-side protections to both the CRM and portfolio
// proxy responses. HSTS is enabled only in production, where the public site is
// served exclusively over HTTPS.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Render probes the service directly, without the Cloudflare Worker
		// header. Keep the health endpoint deliberately minimal and public so
		// origin verification does not make every deployment appear unhealthy.
		if expected := os.Getenv("ORIGIN_VERIFY_SECRET"); expected != "" && r.URL.Path != "/healthz" {
			provided := r.Header.Get("X-Origin-Verify")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
				reason := "header mismatch"
				if provided == "" {
					reason = "header missing"
				}
				log.Printf("origin verification rejected: %s host=%s", reason, r.Host)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		if envOr("APP_ENV", "") == "production" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if r.URL.Path != "/static/" && !strings.HasPrefix(r.URL.Path, "/static/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, HEAD, POST")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
		}
		next.ServeHTTP(w, r)
	})
}

// sameOriginMutationGuard blocks cross-site form submissions and fetches.
// Browser POST requests must include a matching Origin (or Referer). Explicit
// allowed origins are needed because the CRM is served through a Worker proxy
// and can also be reached at its Render origin.
func sameOriginMutationGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
				http.Error(w, "Cross-site request rejected", http.StatusForbidden)
				return
			}
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin == "" {
				if ref := strings.TrimSpace(r.Referer()); ref != "" {
					if parsed, err := url.Parse(ref); err == nil {
						origin = parsed.Scheme + "://" + parsed.Host
					}
				}
			}
			if origin == "" || !isAllowedMutationOrigin(origin, r.Host) {
				http.Error(w, "Request origin could not be verified", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isAllowedMutationOrigin(origin, requestHost string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	allowed := []string{
		"https://dimetrius-eng.tech",
		"https://gadget-store-crm.tech",
		"https://www.gadget-store-crm.tech",
		"https://gadget-store-crm-jj8m.onrender.com",
	}
	allowed = append(allowed, strings.Split(os.Getenv("APP_ALLOWED_ORIGINS"), ",")...)
	for _, candidate := range allowed {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if strings.EqualFold(origin, candidate) {
			return true
		}
	}
	if envOr("APP_ENV", "") != "production" && (strings.EqualFold(parsed.Host, requestHost) || parsed.Host == "localhost" || strings.HasPrefix(parsed.Host, "localhost:")) {
		return parsed.Scheme == "http" || parsed.Scheme == "https"
	}
	return false
}

func sessionTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
