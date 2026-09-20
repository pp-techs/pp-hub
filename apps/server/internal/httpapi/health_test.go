package httpapi

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestReadyzIsRegistered guards the compose and Dockerfile healthchecks,
// which are a contract with a route name: a silent rename here would quietly
// restore "reports healthy while broken" for both /healthz and /readyz.
func TestReadyzIsRegistered(t *testing.T) {
	found := false
	err := chi.Walk(
		(&Server{}).Router().(chi.Routes),
		func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			if method == http.MethodGet && route == "/readyz" {
				found = true
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if !found {
		t.Fatal("GET /readyz is not registered")
	}
}
