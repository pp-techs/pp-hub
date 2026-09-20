// Package mail sends transactional mail over SMTP.
//
// One message exists today, the space invitation, because an invite an admin
// must copy into a chat client is how you onboard a person at a time rather
// than a department. Sharing still notifies nobody — that is M29's half and
// needs a notification story, not a transport.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

var ErrDisabled = errors.New("mail: no transport configured")

// Settings configures an SMTP relay.
type Settings struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	// TLS is "starttls" (default), "implicit" (SMTPS, usually port 465) or
	// "none" (plaintext, for a local catcher in development).
	TLS string
}

// Sender sends mail over the configured relay. A nil *Sender, or one with an
// empty Host, is a supported and common state: Enabled reports false and
// Send returns ErrDisabled rather than panicking or dialing nothing.
type Sender struct{ s Settings }

func New(s Settings) *Sender { return &Sender{s: s} }

// Enabled reports whether a transport is configured. Disabled is a supported
// state, not an error: the invitation handler returns the URL either way.
func (m *Sender) Enabled() bool { return m != nil && m.s.Host != "" }

// Send delivers one plain-text message. Every failure is wrapped with the
// stage it failed at (dial, starttls, auth, …), because "smtp: EOF" alone
// tells an operator nothing about which of five steps broke.
func (m *Sender) Send(ctx context.Context, to, subject, body string) error {
	if !m.Enabled() {
		return ErrDisabled
	}
	// Both reach the message headers verbatim below; an unescaped newline in
	// either is a header-injection attempt (extra To/Bcc, forged headers).
	if strings.ContainsAny(to, "\r\n") || strings.ContainsAny(subject, "\r\n") {
		return errors.New("mail: header injection attempt")
	}

	host := m.s.Host
	addr := net.JoinHostPort(host, strconv.Itoa(m.s.Port))

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if m.s.TLS == "implicit" {
		conn = tls.Client(conn, &tls.Config{ServerName: host})
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	if m.s.TLS == "starttls" {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}

	if m.s.Username != "" {
		// net/smtp refuses PLAIN over an unencrypted non-localhost
		// connection, so TLS=none with credentials only works against a
		// local catcher (mailhog, mailpit) reached on loopback.
		if err := c.Auth(smtp.PlainAuth("", m.s.Username, m.s.Password, host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := c.Mail(m.s.From); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	msg := "From: " + m.s.From + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" + body
	if _, err := wc.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	if err := c.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
	}
	return nil
}
