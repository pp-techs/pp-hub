package mail

import (
	"bytes"
	netmail "net/mail"
	"strings"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		s       Settings
		wantErr string
	}{
		{
			name: "empty host is valid even with invalid other fields",
			s: Settings{
				Host:     "",
				Port:     999999,
				Username: "",
				Password: "pw",
				From:     "not an email",
				TLS:      "bad",
			},
		},
		{
			name: "valid minimal",
			s: Settings{
				Host: "smtp.example.com",
				Port: 587,
				From: "noreply@example.com",
				TLS:  "starttls",
			},
		},
		{
			name: "valid full",
			s: Settings{
				Host:     "smtp.example.com",
				Port:     465,
				Username: "user",
				Password: "secret-password",
				From:     "pp-hub <noreply@example.com>",
				TLS:      "implicit",
			},
		},
		{
			name: "line break in host",
			s: Settings{
				Host: "smtp.example.com\n",
				Port: 587,
				From: "noreply@example.com",
				TLS:  "starttls",
			},
			wantErr: "smtp settings must not contain line breaks",
		},
		{
			name: "line break in password",
			s: Settings{
				Host:     "smtp.example.com",
				Port:     587,
				Username: "user",
				Password: "pw\r\n",
				From:     "noreply@example.com",
				TLS:      "starttls",
			},
			wantErr: "smtp settings must not contain line breaks",
		},
		{
			name: "port too low",
			s: Settings{
				Host: "smtp.example.com",
				Port: 0,
				From: "noreply@example.com",
				TLS:  "starttls",
			},
			wantErr: "smtp port must be between 1 and 65535",
		},
		{
			name: "port too high",
			s: Settings{
				Host: "smtp.example.com",
				Port: 70000,
				From: "noreply@example.com",
				TLS:  "starttls",
			},
			wantErr: "smtp port must be between 1 and 65535",
		},
		{
			name: "bad tls",
			s: Settings{
				Host: "smtp.example.com",
				Port: 587,
				From: "noreply@example.com",
				TLS:  "ssl",
			},
			wantErr: "smtp tls must be starttls, implicit or none",
		},
		{
			name: "empty from",
			s: Settings{
				Host: "smtp.example.com",
				Port: 587,
				From: "",
				TLS:  "starttls",
			},
			wantErr: "a from address is required when an SMTP host is set",
		},
		{
			name: "invalid from address",
			s: Settings{
				Host: "smtp.example.com",
				Port: 587,
				From: "invalid-email@",
				TLS:  "starttls",
			},
			wantErr: "from address is not a valid email address",
		},
		{
			name: "password without username",
			s: Settings{
				Host:     "smtp.example.com",
				Port:     587,
				Username: "",
				Password: "secret",
				From:     "noreply@example.com",
				TLS:      "starttls",
			},
			wantErr: "an SMTP password is set without a username",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.s.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
			} else {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("expected error %q, got %v", tc.wantErr, err)
				}
			}
		})
	}
}

func TestBuildMessageEncodesUnicodeSubject(t *testing.T) {
	from, err := netmail.ParseAddress("pp-hub <noreply@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	to, err := netmail.ParseAddress("user@example.com")
	if err != nil {
		t.Fatal(err)
	}

	longLine := strings.Repeat("A", 100)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	msg := buildMessage(from, to, "Tài liệu", longLine, now)

	s := string(msg)
	if !strings.Contains(s, "Subject: =?utf-8?q?T=C3=A0i_li=E1=BB=87u?=") && !strings.Contains(s, "Subject: =?utf-8?Q?T=C3=A0i_li=E1=BB=87u?=") {
		t.Fatalf("expected encoded unicode subject, got:\n%s", s)
	}
	if !strings.Contains(s, "From: \"pp-hub\" <noreply@example.com>") && !strings.Contains(s, "From: pp-hub <noreply@example.com>") {
		t.Fatalf("expected From header, got:\n%s", s)
	}
	if !strings.Contains(s, "To: <user@example.com>") {
		t.Fatalf("expected To header, got:\n%s", s)
	}
	if !strings.Contains(s, "Date: Wed, 23 Sep 2026 12:00:00 +0000") {
		t.Fatalf("expected Date header, got:\n%s", s)
	}

	// Quoted-printable wraps lines longer than 76 chars with soft line breaks (=\r\n)
	parts := bytes.Split(msg, []byte("\r\n\r\n"))
	if len(parts) < 2 {
		t.Fatalf("expected header and body separated by double CRLF, got:\n%s", s)
	}
	body := string(parts[1])
	if !strings.Contains(body, "=\r\n") {
		t.Fatalf("expected soft-wrapped quoted printable body, got:\n%s", body)
	}
}
