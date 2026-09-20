package mail

import "testing"

func TestEnabledReflectsHost(t *testing.T) {
	if (&Sender{}).Enabled() {
		t.Fatal("a zero-value Settings must not report enabled")
	}
	if !New(Settings{Host: "smtp.example.com"}).Enabled() {
		t.Fatal("a Settings with a Host must report enabled")
	}
	var nilSender *Sender
	if nilSender.Enabled() {
		t.Fatal("a nil *Sender must not report enabled")
	}
}

// Send on a disabled sender must fail fast rather than dial nothing: the
// invitation handler relies on Enabled() to decide whether to call Send at
// all, but a caller that skips the check must still get an actionable error.
func TestSendOnDisabledReturnsErrDisabled(t *testing.T) {
	m := New(Settings{})
	if err := m.Send(t.Context(), "a@example.com", "subject", "body"); err != ErrDisabled {
		t.Fatalf("got %v, want ErrDisabled", err)
	}
}

// A newline in the recipient or subject reaches the raw message headers
// verbatim; refusing it is what stops a caller from injecting an extra
// header (a forged From, a Bcc) into the outgoing message.
func TestSendRejectsHeaderInjection(t *testing.T) {
	m := New(Settings{Host: "smtp.example.com", Port: 25, From: "pp-hub@example.com", TLS: "none"})
	cases := []struct {
		to, subject string
	}{
		{"a@example.com\r\nBcc: victim@example.com", "subject"},
		{"a@example.com", "subject\r\nX-Injected: yes"},
	}
	for _, c := range cases {
		if err := m.Send(t.Context(), c.to, c.subject, "body"); err == nil {
			t.Errorf("to=%q subject=%q: injection accepted", c.to, c.subject)
		}
	}
}
