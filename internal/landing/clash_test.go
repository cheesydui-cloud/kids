package landing

import (
	"strings"
	"testing"
)

func TestClashProxyYAMLVLESS(t *testing.T) {
	uri := "vless://uuid-1@relay.example:10001?security=reality&sni=a.com&pbk=pk&sid=ab&type=tcp#HK"
	y, ok := ClashProxyYAML(uri, "alice-中转")
	if !ok {
		t.Fatal("expected yaml")
	}
	for _, want := range []string{
		`name: "alice-中转"`,
		"type: vless",
		"server: relay.example",
		"port: 10001",
		"uuid: uuid-1",
		"reality-opts:",
	} {
		if !strings.Contains(y, want) {
			t.Errorf("yaml missing %q\n%s", want, y)
		}
	}
}

func TestClashProxyYAMLUnsupported(t *testing.T) {
	if _, ok := ClashProxyYAML("socks5://u:p@1.2.3.4:1080#s", "s"); ok {
		t.Fatal("socks5 should not convert")
	}
}

func TestEncodeURISubscriptionRoundTrip(t *testing.T) {
	uris := []string{
		"vless://a@h:1#one",
		"ss://YWVzLTI1Ni1nY206cGFzcw@h:2#two",
	}
	body := EncodeURISubscription(uris)
	got := DecodeSubscription([]byte(body))
	if len(got) != 2 || got[0] != uris[0] || got[1] != uris[1] {
		t.Fatalf("roundtrip = %#v", got)
	}
}

func TestClashProfileEmpty(t *testing.T) {
	y := ClashProfile(nil)
	if !strings.Contains(y, "proxies:") || !strings.Contains(y, "MATCH,PROXY") {
		t.Fatalf("profile =\n%s", y)
	}
}
