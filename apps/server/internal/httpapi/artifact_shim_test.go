package httpapi

import (
	"strings"
	"testing"
)

func TestInjectStorageShimPlacement(t *testing.T) {
	for name, tc := range map[string]struct{ in, before string }{
		"after head":    {"<!doctype html><html><head><title>x</title></head>", "<!doctype html><html><head>"},
		"head attrs":    {"<HEAD lang=en><meta>", "<HEAD lang=en>"},
		"no head":       {"<html lang=en><body>", "<html lang=en>"},
		"doctype only":  {"<!DOCTYPE html>\n<p>hi", "<!DOCTYPE html>"},
		"bare fragment": {"<p>hi</p>", ""},
	} {
		got := string(injectStorageShim([]byte(tc.in)))
		if !strings.HasPrefix(got, tc.before+"<script>") || !strings.HasSuffix(got, strings.TrimPrefix(tc.in, tc.before)) {
			t.Errorf("%s: %q", name, got)
		}
	}
}
