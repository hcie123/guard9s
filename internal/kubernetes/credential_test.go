package kubernetes

import (
	"errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"net/http"
	"strings"
	"testing"
)

func TestCredentialErrorsNeverExposeRawCredentials(t *testing.T) {
	marker := "Authorization: Bearer synthetic-sensitive-token\n-----BEGIN PRIVATE KEY-----\nsynthetic-key\nclient-certificate-data: synthetic-cert"
	for _, err := range []error{errors.New(marker), apierrors.NewUnauthorized(marker), apierrors.NewResourceExpired(marker)} {
		safe := sanitizedStatus(err).Error() + classify(err)
		for _, secret := range []string{"synthetic-sensitive-token", "synthetic-key", "synthetic-cert", "BEGIN PRIVATE KEY", "Authorization:"} {
			if strings.Contains(safe, secret) {
				t.Fatal("credential leaked")
			}
		}
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		req, _ := http.NewRequest(method, "https://example.invalid/api?credential=synthetic-sensitive-token", nil)
		req.Header.Set("Authorization", "Bearer synthetic-sensitive-token")
		_, err := (readOnlyTransport{}).RoundTrip(req)
		if err == nil || strings.Contains(err.Error(), "synthetic-sensitive-token") {
			t.Fatal("credential leaked in transport error")
		}
	}
}
func TestTransportAllowsWatchGET(t *testing.T) {
	calls := 0
	rt := readOnlyTransport{base: roundTripFunc(func(req *http.Request) (*http.Response, error) { calls++; return &http.Response{StatusCode: 200}, nil })}
	req, _ := http.NewRequest("GET", "https://example.invalid/api/v1/pods?watch=true", nil)
	if _, err := rt.RoundTrip(req); err != nil || calls != 1 {
		t.Fatal("watch GET blocked", err)
	}
}

func TestProtectedTransportPreservesCancellation(t *testing.T) {
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("unused") })
	cfg := Protect(&rest.Config{})
	wrapped := cfg.WrapTransport(base)
	wrapper, ok := wrapped.(interface{ WrappedRoundTripper() http.RoundTripper })
	if !ok || wrapper.WrappedRoundTripper() == nil {
		t.Fatal("timeout cancellation traversal lost")
	}
}
