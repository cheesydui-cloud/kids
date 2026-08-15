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

func TestClashProxyYAMLSocks5(t *testing.T) {
	y, ok := ClashProxyYAML("socks5://alice:s3cret@relay.example:20001#S5", "test-8月30日")
	if !ok {
		t.Fatal("socks5 should convert")
	}
	for _, want := range []string{
		`name: "test-8月30日"`,
		"type: socks5",
		"server: relay.example",
		"port: 20001",
		`username: "alice"`,
		`password: "s3cret"`,
	} {
		if !strings.Contains(y, want) {
			t.Errorf("yaml missing %q\n%s", want, y)
		}
	}
}

func TestClashProfilePacksMixedProtocols(t *testing.T) {
	y := ClashProfile([]NamedURI{
		{Name: "ss-1", URI: "ss://YWVzLTI1Ni1nY206cGFzcw==@1.2.3.4:443#ss"},
		{Name: "s5-2", URI: "socks5://u:p@relay.example:10002#s5"},
		{Name: "vl-3", URI: "vless://uuid@relay.example:10003?security=tls&sni=a.com#vl"},
	})
	if strings.Count(y, "type: ") < 3 {
		t.Fatalf("expected 3 proxies:\n%s", y)
	}
	if !strings.Contains(y, "type: ss") || !strings.Contains(y, "type: socks5") || !strings.Contains(y, "type: vless") {
		t.Fatalf("missing types:\n%s", y)
	}
	if !strings.Contains(y, `- "ss-1"`) || !strings.Contains(y, `- "s5-2"`) || !strings.Contains(y, `- "vl-3"`) {
		t.Fatalf("proxy-groups missing names:\n%s", y)
	}
}

func TestClashProxyYAMLUnsupported(t *testing.T) {
	if _, ok := ClashProxyYAML("mieru://u:p@1.2.3.4:1080#s", "s"); ok {
		t.Fatal("mieru should not convert")
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
