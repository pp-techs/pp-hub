package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

var ErrDisabled = errors.New("mail: no SMTP relay configured")

// Settings is one SMTP relay. Host == "" means mail is off — a supported state.
type Settings struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // RFC 5322 address, optionally with a display name: `pp-hub <noreply@example.com>`
	TLS      string // "starttls" | "implicit" | "none"
}

func (s Settings) Enabled() bool { return s.Host != "" }

// Validate reports a half-configured relay. Host == "" is always valid.
func (s Settings) Validate() error {
	if s.Host == "" {
		return nil
	}
	for _, f := range []string{s.Host, s.Username, s.Password, s.From, s.TLS} {
		if strings.ContainsAny(f, "\r\n") {
			return errors.New("smtp settings must not contain line breaks")
		}
	}
	if s.Port < 1 || s.Port > 65535 {
		return errors.New("smtp port must be between 1 and 65535")
	}
	switch s.TLS {
	case "starttls", "implicit", "none":
	default:
		return errors.New("smtp tls must be starttls, implicit or none")
	}
	if s.From == "" {
		return errors.New("a from address is required when an SMTP host is set")
	}
	if _, err := netmail.ParseAddress(s.From); err != nil {
		return errors.New("from address is not a valid email address")
	}
	if s.Password != "" && s.Username == "" {
		return errors.New("an SMTP password is set without a username")
	}
	return nil
}

type Message struct {
	To      string
	Subject string
	Body    string
}

type Sender struct {
	settings func() (Settings, error)
}

// New takes a provider, not a value: the settings are admin-editable at runtime.
func New(settings func() (Settings, error)) *Sender {
	return &Sender{settings: settings}
}

// Enabled is nil-safe; a provider error counts as disabled.
func (m *Sender) Enabled() bool {
	if m == nil || m.settings == nil {
		return false
	}
	s, err := m.settings()
	if err != nil {
		return false
	}
	return s.Enabled()
}

func (m *Sender) Send(ctx context.Context, msg Message) error {
	if m == nil || m.settings == nil {
		return ErrDisabled
	}
	s, err := m.settings()
	if err != nil {
		return err
	}
	if !s.Enabled() {
		return ErrDisabled
	}

	fromAddr, err := netmail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("mail: invalid from address: %w", err)
	}
	toAddr, err := netmail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("mail: invalid to address: %w", err)
	}

	if strings.ContainsAny(msg.To, "\r\n") || strings.ContainsAny(msg.Subject, "\r\n") {
		return errors.New("mail: header injection attempt")
	}

	hostPort := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	var conn net.Conn
	if s.TLS == "implicit" {
		dialer := &tls.Dialer{
			NetDialer: &net.Dialer{},
			Config: &tls.Config{
				ServerName: s.Host,
				MinVersion: tls.VersionTLS12,
			},
		}
		conn, err = dialer.DialContext(ctx, "tcp", hostPort)
	} else {
		dialer := &net.Dialer{}
		conn, err = dialer.DialContext(ctx, "tcp", hostPort)
	}
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	_ = conn.SetDeadline(deadline)

	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer c.Close()

	parts := strings.Split(fromAddr.Address, "@")
	domain := "localhost"
	if len(parts) > 1 && parts[1] != "" {
		domain = parts[1]
	}

	if err := c.Hello(domain); err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}

	if s.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp starttls: server does not offer STARTTLS")
		}
		if err := c.StartTLS(&tls.Config{
			ServerName: s.Host,
			MinVersion: tls.VersionTLS12,
		}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}

	if s.Username != "" {
		// net/smtp refuses PLAIN over plaintext except to localhost.
		auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := c.Mail(fromAddr.Address); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := c.Rcpt(toAddr.Address); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}

	msgBytes := buildMessage(fromAddr, toAddr, msg.Subject, msg.Body, time.Now())
	if _, err := w.Write(msgBytes); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}

	_ = c.Quit()
	return nil
}

func buildMessage(from *netmail.Address, to *netmail.Address, subject, body string, now time.Time) []byte {
	fromDomain := "localhost"
	parts := strings.Split(from.Address, "@")
	if len(parts) > 1 && parts[1] != "" {
		fromDomain = parts[1]
	}

	var randomBytes [16]byte
	_, _ = rand.Read(randomBytes[:])
	msgID := hex.EncodeToString(randomBytes[:])

	var b bytes.Buffer
	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + to.String() + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("Date: " + now.Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: <" + msgID + "@" + fromDomain + ">\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	b.WriteString("\r\n")

	qw := quotedprintable.NewWriter(&b)
	_, _ = qw.Write([]byte(body))
	_ = qw.Close()

	return b.Bytes()
}
