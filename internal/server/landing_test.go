package server

import (
	"testing"

	"nft/internal/db"
	"nft/internal/landing"
)

func TestClassifyExit(t *testing.T) {
	idx := map[string]landing.Node{
		"1.2.3.4:443": {Name: "HK-01", Protocol: "vless", Host: "1.2.3.4", Port: 443,
			URI: "vless://uuid@1.2.3.4:443?security=reality&sni=a.com#HK-01"},
	}

	t.Run("landing match yields relay uri with entry endpoint", func(t *testing.T) {
		idxWithExp := map[string]landing.Node{
			"1.2.3.4:443": {Name: "HK-01", Protocol: "vless", Host: "1.2.3.4", Port: 443,
				URI: "vless://uuid@1.2.3.4:443?security=reality&sni=a.com#HK-01", ExpiresAt: 1_787_000_000},
		}
		it := ruleListItem{Rule: &db.Rule{ExitHost: "1.2.3.4", ExitPort: 443},
			Entry: "relay.example:10001", Exit: "1.2.3.4:443"}
		it.classifyExit(idxWithExp, true)
		if it.ExitKind != "landing" {
			t.Fatalf("kind = %q, want landing", it.ExitKind)
		}
		if it.LandingName != "HK-01" {
			t.Errorf("landing_name = %q", it.LandingName)
		}
		if it.LandingProtocol != "vless" {
			t.Errorf("landing_protocol = %q, want vless", it.LandingProtocol)
		}
		if it.LandingExpiresAt != 1_787_000_000 {
			t.Errorf("landing_expires_at = %d, want user-exit expiry", it.LandingExpiresAt)
		}
		want := "vless://uuid@relay.example:10001?security=reality&sni=a.com#HK-01"
		if it.RelayURI != want {
			t.Errorf("relay_uri = %q, want %q", it.RelayURI, want)
		}
	})

	t.Run("admin list (withURI=false) marks kind but omits relay uri", func(t *testing.T) {
		it := ruleListItem{Rule: &db.Rule{ExitHost: "1.2.3.4", ExitPort: 443},
			Entry: "relay.example:10001", Exit: "1.2.3.4:443"}
		it.classifyExit(idx, false)
		if it.ExitKind != "landing" || it.RelayURI != "" {
			t.Fatalf("kind=%q relay=%q, want landing with empty relay", it.ExitKind, it.RelayURI)
		}
	})

	t.Run("custom exit has no relay uri (user URIs are client-side)", func(t *testing.T) {
		it := ruleListItem{Rule: &db.Rule{ExitHost: "9.9.9.9", ExitPort: 8443},
			Entry: "relay.example:20000", Exit: "9.9.9.9:8443"}
		it.classifyExit(idx, true)
		if it.ExitKind != "custom" || it.RelayURI != "" || it.LandingURI != "" {
			t.Fatalf("got kind=%q relay=%q landing=%q", it.ExitKind, it.RelayURI, it.LandingURI)
		}
	})

		t.Run("no entry yet skips relay uri", func(t *testing.T) {
			it := ruleListItem{Rule: &db.Rule{ExitHost: "1.2.3.4", ExitPort: 443},
				Entry: "—", Exit: "1.2.3.4:443"}
			it.classifyExit(idx, true)
			if it.ExitKind != "landing" || it.RelayURI != "" {
				t.Fatalf("kind=%q relay=%q, want landing with empty relay", it.ExitKind, it.RelayURI)
			}
		})

		t.Run("domain entry omits raw ipv6 twin", func(t *testing.T) {
			it := ruleListItem{Rule: &db.Rule{ExitHost: "1.2.3.4", ExitPort: 443},
				Entry: "per-year.cnodelink.com:10401", EntryV6: "[2001:db8::1]:10401", Exit: "1.2.3.4:443"}
			it.classifyExit(idx, true)
			if it.RelayURI == "" {
				t.Fatal("want domain relay_uri")
			}
			if it.RelayURIV6 != "" {
				t.Fatalf("relay_uri_v6 = %q, want empty when entry is a hostname", it.RelayURIV6)
			}
		})

		t.Run("bare ipv4 entry still emits ipv6 twin", func(t *testing.T) {
			it := ruleListItem{Rule: &db.Rule{ExitHost: "1.2.3.4", ExitPort: 443},
				Entry: "10.0.0.1:10401", EntryV6: "[2001:db8::1]:10401", Exit: "1.2.3.4:443"}
			it.classifyExit(idx, true)
			if it.RelayURIV6 == "" {
				t.Fatal("want relay_uri_v6 for dual-stack IP entry")
			}
		})
	}
