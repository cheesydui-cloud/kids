// Package tlsfallback installs a Mozilla CA bundle for processes that have no
// system certificate store. debian:bookworm-slim is the common case: HTTPS to
// Cloudflare and GitHub fails with "certificate signed by unknown authority"
// until ca-certificates is installed. SetFallbackRoots is a no-op when the
// host already has a usable system pool.
package tlsfallback

import (
	"crypto/x509"
	_ "embed"
	"log"
)

//go:embed cacert.pem
var cacertPEM []byte

func init() {
	if err := Install(); err != nil {
		log.Printf("tls fallback roots: %v", err)
	}
}

// Install parses the embedded Mozilla bundle and registers it with crypto/x509.
// It must run before the first HTTPS request. A second call panics; init calls
// it once.
func Install() error {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cacertPEM) {
		return errEmptyBundle
	}
	x509.SetFallbackRoots(pool)
	return nil
}

type emptyBundleError struct{}

func (emptyBundleError) Error() string { return "embedded CA bundle has no certificates" }

var errEmptyBundle error = emptyBundleError{}
