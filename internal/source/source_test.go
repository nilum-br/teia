package source

import (
	"reflect"
	"strings"
	"testing"
)

func TestSensitiveLabel(t *testing.T) {
	cases := []struct {
		key, value string
		sensitive  bool
	}{
		{"app", "orders", false},
		{"app.kubernetes.io/name", "orders", false},
		{"team", "payments", false},
		{"traefik.http.middlewares.auth.basicauth.users", "admin:$apr1$x", true},
		{"traefik.http.middlewares.auth.basicAuth.users", "admin:$apr1$x", true},
		{"caddy.basic_auth.users", "admin x", true},
		{"DB_PASSWORD", "hunter2", true},
		{"mysql.passwd", "hunter2", true},
		{"vault.secret-id", "x", true},
		{"github.token", "ghp_x", true},
		{"datadog.apiKey", "x", true},
		{"service.api-key", "x", true},
		{"service.api_key", "x", true},
		{"aws.credentials", "x", true},
		{"tls.private-key", "x", true},
		{"cert-manager.io/issuer", "letsencrypt", true},
		{"kubectl.kubernetes.io/last-applied-configuration", "{}", true},
		{"description", strings.Repeat("x", MaxLabelValue), false},
		{"description", strings.Repeat("x", MaxLabelValue+1), true},
	}
	for _, c := range cases {
		if got := SensitiveLabel(c.key, c.value); got != c.sensitive {
			t.Errorf("SensitiveLabel(%q, %.20q) = %v, want %v", c.key, c.value, got, c.sensitive)
		}
	}
}

func TestSafeLabels(t *testing.T) {
	got := SafeLabels(map[string]string{
		"app": "orders",
		"traefik.http.middlewares.auth.basicauth.users": "admin:$apr1$x",
		"notes": strings.Repeat("x", MaxLabelValue+1),
	})
	if want := map[string]string{"app": "orders"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SafeLabels = %v, want %v", got, want)
	}
	if SafeLabels(nil) == nil {
		t.Error("SafeLabels(nil) must return an empty map callers can add to")
	}
}
