package proxyrequest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTakeIdentityAcceptsCanonicalUUIDv7AndRemovesHeader(t *testing.T) {
	const retryID = "01982d4f-7c2a-7abc-9d43-1a2b3c4d5e6f"
	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil)
	req.Header.Set(RetryIDHeader, retryID)
	identity, err := TakeIdentity(req)
	if err != nil || !identity.Valid() || identity.RetryID != retryID || identity.Digest() == [32]byte{} {
		t.Fatalf("unexpected identity: identity=%#v err=%v", identity, err)
	}
	if req.Header.Get(RetryIDHeader) != "" {
		t.Fatal("retry ID was not removed from the request")
	}
}

func TestParseRetryIDRejectsNonCanonicalOrWrongVersion(t *testing.T) {
	for _, raw := range []string{
		"",
		"01982D4F-7C2A-7ABC-9D43-1A2B3C4D5E6F",
		"01982d4f7c2a7abc9d431a2b3c4d5e6f",
		"01982d4f-7c2a-4abc-9d43-1a2b3c4d5e6f",
		"01982d4f-7c2a-7abc-1d43-1a2b3c4d5e6f",
	} {
		if _, err := ParseRetryID(raw); err == nil {
			t.Fatalf("invalid retry ID was accepted: %q", raw)
		}
	}
}
