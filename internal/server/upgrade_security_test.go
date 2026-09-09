package server

import (
	"net/http/httptest"
	"testing"
)

func TestRequestOriginRejectsUntrustedOrUnsafeForwardedHost(t *testing.T) {
	r := httptest.NewRequest("GET", "http://panel.example/v1/install-agent", nil)
	r.Header.Set("X-Forwarded-Host", "panel.example'; touch /tmp/pwned; echo '")
	if got := requestOrigin(r); got != "http://panel.example" {
		t.Fatalf("untrusted forwarded host changed origin: %q", got)
	}

	r = httptest.NewRequest("GET", "http://panel.example/v1/install-agent", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-Host", "panel.example'; touch /tmp/pwned; echo '")
	if got := requestOrigin(r); got != "" {
		t.Fatalf("unsafe trusted forwarded host accepted: %q", got)
	}
}

func TestValidOriginHost(t *testing.T) {
	for _, tc := range []struct {
		host string
		ok   bool
	}{
		{"panel.example:7788", true},
		{"[::1]:7788", true},
		{"panel.example/path", false},
		{"panel.example';echo pwned'", false},
	} {
		if got := validOriginHost(tc.host); got != tc.ok {
			t.Errorf("validOriginHost(%q) = %v, want %v", tc.host, got, tc.ok)
		}
	}
}
