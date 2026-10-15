package api

import (
	"net/http"
	"strings"
)

// allowOrigins lets the listed browser origins read /api/v1/* (including the
// stream) from another site, such as a Grafana panel streaming straight from
// teia. Origins match exactly; "*" must be listed explicitly. Credentials
// (cookies) are never allowed across sites: other origins authenticate with
// a bearer token, which preflights may ask to send when auth is on.
func allowOrigins(next http.Handler, origins []string, withAuth bool) http.Handler {
	if len(origins) == 0 {
		return next
	}
	allowed := map[string]bool{}
	anyOrigin := false
	for _, o := range origins {
		if o == "*" {
			anyOrigin = true
			continue
		}
		allowed[strings.ToLower(strings.TrimRight(o, "/"))] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Add("Vary", "Origin")
		preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
		if !anyOrigin && !allowed[strings.ToLower(origin)] {
			if preflight {
				writeError(w, http.StatusForbidden, "origin not allowed: add it to cors.allowedOrigins in teia.yaml")
				return
			}
			next.ServeHTTP(w, r) // without CORS headers the browser keeps the answer from the page
			return
		}
		if anyOrigin {
			h.Set("Access-Control-Allow-Origin", "*")
		} else {
			h.Set("Access-Control-Allow-Origin", origin)
		}
		if !preflight {
			next.ServeHTTP(w, r)
			return
		}
		h.Add("Vary", "Access-Control-Request-Method")
		h.Add("Vary", "Access-Control-Request-Headers")
		h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		if r.URL.Path == "/api/v1/review/proposals" {
			h.Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if withAuth {
			if r.URL.Path == "/api/v1/review/proposals" {
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			} else {
				h.Set("Access-Control-Allow-Headers", "Authorization")
			}
		}
		h.Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
	})
}
