package landing

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type statusRoundTripper struct{ status int }

func (rt statusRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: rt.status,
		Status:     "500 Internal Server Error",
		Body:       io.NopCloser(strings.NewReader("not a subscription")),
		Header:     make(http.Header),
	}, nil
}

func TestFetchRejectsNon2xxResponse(t *testing.T) {
	f := &Fetcher{client: &http.Client{Transport: statusRoundTripper{status: http.StatusInternalServerError}}, cache: map[string]cacheEntry{}}
	if _, err := f.fetch("https://example.invalid/sub"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("fetch error = %v, want HTTP 500", err)
	}
}
