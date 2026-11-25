package tlsfallback

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestEmbeddedBundleParses(t *testing.T) {
	rest := cacertPEM
	n := 0
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			t.Fatalf("cert %d: %v", n, err)
		}
		n++
	}
	if n < 100 {
		t.Fatalf("embedded bundle has %d certificates, want at least 100", n)
	}
}
