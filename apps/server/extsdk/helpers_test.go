package extsdk_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/nhatbui/pp-hub/apps/server/extsdk"
)

func TestMustRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic outside Guard")
			}
		}()
		extsdk.MustRequest(r)
	}()
	r = r.WithContext(extsdk.WithRequest(context.Background(), extsdk.Request{Caps: extsdk.Caps{}}))
	_ = extsdk.MustRequest(r)
}

func TestFail(t *testing.T) {
	tests := []struct {
		err    error
		status int
		body   string
	}{
		{extsdk.Conflict("column is full"), http.StatusConflict, "column is full"},
		{fmt.Errorf("wrap: %w", extsdk.Conflict("again")), http.StatusConflict, "again"},
		{pgx.ErrNoRows, http.StatusNotFound, "not found"},
		{fmt.Errorf("wrap: %w", pgx.ErrNoRows), http.StatusNotFound, "not found"},
		{errors.New("boom"), http.StatusInternalServerError, ""},
	}
	for _, tc := range tests {
		w := httptest.NewRecorder()
		extsdk.Fail(w, tc.err)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) {
			t.Errorf("%v: got %d %s", tc.err, w.Code, w.Body)
		}
		if tc.status == 500 && strings.Contains(w.Body.String(), "boom") {
			t.Error("500 must not leak the error")
		}
	}
}

func TestTrim(t *testing.T) {
	tests := []struct {
		in       string
		max      int
		required bool
		want     string
		ok       bool
		status   int
	}{
		{"  hi  ", 5, true, "hi", true, 200},
		{"   ", 5, true, "", false, 400},
		{"   ", 5, false, "", true, 200},
		{"héllo", 5, true, "héllo", true, 200},
		{"héllos", 5, true, "", false, 400},
	}
	for _, tc := range tests {
		w := httptest.NewRecorder()
		got, ok := extsdk.Trim(w, tc.in, "title", tc.max, tc.required)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%q: got %q,%v", tc.in, got, ok)
		}
		if !ok && w.Code != tc.status {
			t.Errorf("%q: status %d", tc.in, w.Code)
		}
	}
}
