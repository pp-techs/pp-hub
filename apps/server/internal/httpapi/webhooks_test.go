package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sign(key, body string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

// The signature is the only credential this route has: it is mounted outside
// the authenticated group precisely because the caller has no session. Every
// way of getting it wrong must be refused before the body is parsed, let
// alone acted on.
func TestLogtoWebhookRefusesEverythingButAValidSignature(t *testing.T) {
	const key = "signing-key"
	const body = `{"event":"Organization.Membership.Updated","organizationId":"org_1"}`
	s := &Server{webhookKey: key}

	cases := map[string]string{
		"no signature":                  "",
		"wrong key":                     sign("other-key", body),
		"signature of a different body": sign(key, body+" "),
		"not hex":                       "zzzz",
	}
	for name, signature := range cases {
		req := httptest.NewRequest(http.MethodPost, "/hooks/logto", strings.NewReader(body))
		if signature != "" {
			req.Header.Set("logto-signature-sha-256", signature)
		}
		w := httptest.NewRecorder()
		s.handleLogtoWebhook(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, w.Code)
		}
	}
}

// An unset signing key must close the route rather than open it. The hourly
// reconcile still converges without webhooks, so failing closed costs
// latency; failing open would hand an unauthenticated caller a lever on
// membership.
func TestLogtoWebhookIsClosedWithoutASigningKey(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/hooks/logto", strings.NewReader(`{}`))
	req.Header.Set("logto-signature-sha-256", sign("", "{}"))
	w := httptest.NewRecorder()
	s.handleLogtoWebhook(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// A correctly signed delivery for an event that names no organization is
// acknowledged, not rejected: a 4xx makes Logto retry something that can
// never apply. It must also not reach the enqueue path, which would panic on
// this Server's nil orgsync.
func TestLogtoWebhookAcknowledgesDeliveriesItCannotActOn(t *testing.T) {
	const key = "signing-key"
	const body = `{"event":"User.Created"}`
	s := &Server{webhookKey: key}
	req := httptest.NewRequest(http.MethodPost, "/hooks/logto", strings.NewReader(body))
	req.Header.Set("logto-signature-sha-256", sign(key, body))
	w := httptest.NewRecorder()
	s.handleLogtoWebhook(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}
