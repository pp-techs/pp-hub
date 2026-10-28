// office-agent is a credential-free Unix-socket worker. It must run without
// network access, application secrets, database credentials or the CAS mount.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nhatbui/pp-hub/apps/server/internal/officeedit"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	socket := flag.String("socket", env("PP_OFFICE_AGENT_SOCKET", "/run/office-agent/office-agent.sock"), "Unix socket path")
	binary := flag.String("binary", env("OFFICECLI_BINARY", "officecli"), "OfficeCLI executable")
	health := flag.Bool("healthcheck", false, "check the runner through its Unix socket and exit")
	flag.Parse()
	if flag.NArg() != 0 || !filepath.IsAbs(*socket) {
		return errors.New("office-agent requires an absolute Unix socket path")
	}
	if *health {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := officeedit.NewClient(*socket).Do(ctx, officeedit.Request{Action: "schema", Format: "docx"})
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*socket), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(*socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("refusing to replace a non-socket office-agent path")
		}
		conn, dialErr := net.DialTimeout("unix", *socket, time.Second)
		if dialErr == nil {
			conn.Close()
			return errors.New("office-agent socket is already in use")
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, os.ErrNotExist) {
			return errors.New("cannot determine whether office-agent socket is in use")
		}
		if err := os.Remove(*socket); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", *socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(*socket, 0600); err != nil {
		return err
	}
	ownSocket, err := os.Lstat(*socket)
	if err != nil {
		return err
	}
	defer func() {
		if current, err := os.Lstat(*socket); err == nil && os.SameFile(ownSocket, current) {
			_ = os.Remove(*socket)
		}
	}()
	fmt.Fprintln(os.Stderr, "office-agent listening on", *socket)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Handler: officeedit.Handler(*binary), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 55 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if server.Shutdown(shutdown) != nil {
			_ = server.Close()
		}
	}()
	err = server.Serve(listener)
	stop()
	<-stopped
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
