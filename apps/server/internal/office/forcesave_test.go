package office

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeDocServer stands in for the Document Server's command service. It
// verifies the signature the way the real one does (HS256 over the body's
// `token`, and over the header's `payload` wrapper), then answers with the
// error code the test scripted for that key.
type fakeDocServer struct {
	t      *testing.T
	secret []byte
	codes  map[string]int
	seen   []string
}

func (f *fakeDocServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/command" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Errorf("body is not a command: %v", err)
		http.Error(w, "bad", http.StatusBadRequest)
		return
	}
	if len(f.secret) > 0 {
		if !f.verified(req.Token, req.C, req.Key) {
			_ = json.NewEncoder(w).Encode(commandResponse{Error: 6})
			return
		}
		hdr := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !f.verifiedWrapped(hdr, req.C, req.Key) {
			_ = json.NewEncoder(w).Encode(commandResponse{Error: 6})
			return
		}
	} else if req.Token != "" || r.Header.Get("Authorization") != "" {
		f.t.Errorf("unsigned Document Server was sent a credential")
	}
	f.seen = append(f.seen, req.C+" "+req.Key)
	_ = json.NewEncoder(w).Encode(commandResponse{Error: f.codes[req.Key]})
}

func (f *fakeDocServer) claims(token string) (map[string]any, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	mac := hmac.New(sha256.New, f.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if parts[2] != base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil, false
	}
	return m, true
}

func (f *fakeDocServer) verified(token, c, key string) bool {
	m, ok := f.claims(token)
	return ok && m["c"] == c && m["key"] == key
}

func (f *fakeDocServer) verifiedWrapped(token, c, key string) bool {
	m, ok := f.claims(token)
	if !ok {
		return false
	}
	p, _ := m["payload"].(map[string]any)
	return p["c"] == c && p["key"] == key
}

func newSaver(t *testing.T, secret []byte, codes map[string]int, keys ...string) (*Forcesaver, *fakeDocServer) {
	t.Helper()
	fake := &fakeDocServer{t: t, secret: secret, codes: codes}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return &Forcesaver{
		URL:    srv.URL + "/",
		Secret: secret,
		Keys:   func(context.Context) ([]string, error) { return keys, nil },
	}, fake
}

// The Document Server is signed in every real deployment, and it answers a
// wrong signature with a code that looks like any other failure, so this is
// the check that the token is actually one it accepts: body and header form.
func TestForcesaveIsSignedForASignedDocumentServer(t *testing.T) {
	f, fake := newSaver(t, []byte("a-shared-onlyoffice-secret-value"), nil, "wopi_abc")

	saved, err := f.Once(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if saved != 1 || len(fake.seen) != 1 || fake.seen[0] != "forcesave wopi_abc" {
		t.Errorf("saved=%d, server saw %v", saved, fake.seen)
	}
}

func TestForcesaveWithAWrongSecretIsAnError(t *testing.T) {
	f, fake := newSaver(t, []byte("the-document-servers-secret-000"), nil, "wopi_abc")
	f.Secret = []byte("not-the-document-servers-secret")

	if _, err := f.Once(t.Context()); err == nil {
		t.Error("a rejected signature (error 6) was treated as success")
	}
	if len(fake.seen) != 0 {
		t.Errorf("an unsigned-in command was executed: %v", fake.seen)
	}
}

func TestForcesaveToAnUnsignedDocumentServerSendsNoCredential(t *testing.T) {
	f, fake := newSaver(t, nil, nil, "wopi_abc")
	if _, err := f.Once(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(fake.seen) != 1 {
		t.Errorf("server saw %v", fake.seen)
	}
}

// A lock that outlived its editing session, and a document nobody has
// touched, are the two ordinary answers on every round. Neither may be
// reported as a failure or the log would fill with them.
func TestForcesaveTreatsGoneSessionsAndUnchangedDocumentsAsNormal(t *testing.T) {
	f, _ := newSaver(t, nil, map[string]int{"wopi_gone": 1, "wopi_idle": 4}, "wopi_gone", "wopi_idle", "wopi_busy")

	saved, err := f.Once(t.Context())
	if err != nil {
		t.Fatalf("normal answers reported as an error: %v", err)
	}
	if saved != 1 {
		t.Errorf("saved = %d, want 1 (only the document with changes)", saved)
	}
}

// One document the Document Server cannot save must not stop the others from
// being saved, and the failure still has to reach the log.
func TestForcesaveContinuesPastAFailingDocument(t *testing.T) {
	f, fake := newSaver(t, nil, map[string]int{"wopi_bad": 3}, "wopi_bad", "wopi_good")

	saved, err := f.Once(t.Context())
	if err == nil {
		t.Error("error 3 was swallowed")
	}
	if saved != 1 || len(fake.seen) != 2 {
		t.Errorf("saved=%d, server saw %v; the second document was skipped", saved, fake.seen)
	}
}

func TestForcesaveReportsAFailedLockListing(t *testing.T) {
	f := &Forcesaver{URL: "http://unused", Keys: func(context.Context) ([]string, error) {
		return nil, errors.New("db down")
	}}
	if _, err := f.Once(t.Context()); err == nil {
		t.Error("a failed lock listing was reported as an empty round")
	}
}

func TestDropSessionTerminatesSessionAndTreatsGoneAsNormal(t *testing.T) {
	f, fake := newSaver(t, []byte("shared-secret"), map[string]int{"wopi_gone": 1}, "wopi_active", "wopi_gone")

	if err := f.Drop(t.Context(), "wopi_active"); err != nil {
		t.Fatalf("drop active session: %v", err)
	}
	if err := f.Drop(t.Context(), "wopi_gone"); err != nil {
		t.Fatalf("drop already-gone session: %v", err)
	}
	if len(fake.seen) != 2 || fake.seen[0] != "drop wopi_active" || fake.seen[1] != "drop wopi_gone" {
		t.Errorf("server saw %v, want drop commands", fake.seen)
	}
}
