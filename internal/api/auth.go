package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

// sessionCookie carries the UI's credentials: EventSource cannot send an
// Authorization header, so a browser opens /?token=... once and gets this
// HttpOnly cookie instead.
const sessionCookie = "teia_session"

// requireToken protects every endpoint but /healthz with token, when set.
// Scripts, Grafana and Prometheus send "Authorization: Bearer <token>";
// browsers use the cookie. Comparisons are constant-time.
func requireToken(next http.Handler, token string) http.Handler {
	if token == "" {
		return next
	}
	// The cookie holds a value derived from the token, not the token itself,
	// so it cannot be replayed as a bearer token elsewhere.
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("teia ui session"))
	session := hex.EncodeToString(mac.Sum(nil))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/" && r.URL.Query().Has("token") {
			if !same(r.URL.Query().Get("token"), token) {
				unauthorized(w)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: sessionCookie, Value: session, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: overTLS(r),
			})
			// Drop the token from the address bar and history. The Location
			// is relative, so it also works behind a path-prefix proxy.
			q := r.URL.Query()
			q.Del("token")
			loc := "./"
			if len(q) > 0 {
				loc += "?" + q.Encode()
			}
			w.Header().Set("Location", loc)
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusSeeOther)
			return
		}
		if bearer, ok := bearerToken(r); ok && same(bearer, token) {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(sessionCookie); err == nil && same(c.Value, session) {
			next.ServeHTTP(w, r)
			return
		}
		unauthorized(w)
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="teia"`)
	writeError(w, http.StatusUnauthorized,
		"missing or invalid token: send Authorization: Bearer <token>, or open /?token=<token> once in a browser")
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return strings.TrimSpace(value), true
}

// same compares in constant time; hashing first hides the length too.
func same(got, want string) bool {
	g, w := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(g[:], w[:]) == 1
}

// overTLS reports whether the browser reached teia over HTTPS, directly or
// through a proxy that terminates TLS.
func overTLS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
