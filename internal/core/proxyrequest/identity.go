package proxyrequest

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
)

const (
	RequestIDHeader        = "Dproxy-Request-Id"
	RetryCapabilityHeader  = "Dproxy-Retry-Capability"
	RetryAuthorizationName = "DProxy-Retry"
)

var ErrInvalidIdentity = errors.New("invalid proxy request retry identity")

type Identity struct {
	RequestID string
	digest    [sha256.Size]byte
	valid     bool
}

func TakeIdentity(req *http.Request) (Identity, error) {
	if req == nil {
		return Identity{}, nil
	}
	requestID := strings.TrimSpace(req.Header.Get(RequestIDHeader))
	capability := strings.TrimSpace(req.Header.Get(RetryCapabilityHeader))
	req.Header.Del(RequestIDHeader)
	req.Header.Del(RetryCapabilityHeader)
	if requestID == "" && capability == "" {
		return Identity{}, nil
	}
	requestIDBytes, err := base64.RawURLEncoding.DecodeString(requestID)
	if err != nil || len(requestIDBytes) != 16 || capability == "" {
		return Identity{}, ErrInvalidIdentity
	}
	capabilityBytes, err := base64.RawURLEncoding.DecodeString(capability)
	if err != nil || len(capabilityBytes) != 32 {
		return Identity{}, ErrInvalidIdentity
	}
	return Identity{RequestID: requestID, digest: hashIdentity(requestIDBytes, capabilityBytes), valid: true}, nil
}

func (i Identity) Valid() bool { return i.valid && i.RequestID != "" }

func (i Identity) Digest() [sha256.Size]byte { return i.digest }

func (i Identity) HasRequestID() bool { return i.RequestID != "" }

func ParseRetryAuthorization(value string) (requestID, capability string, err error) {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], RetryAuthorizationName) {
		return "", "", ErrInvalidIdentity
	}
	encoded := strings.Split(parts[1], ".")
	if len(encoded) != 2 {
		return "", "", ErrInvalidIdentity
	}
	if decoded, decodeErr := base64.RawURLEncoding.DecodeString(encoded[0]); decodeErr != nil || len(decoded) != 16 {
		return "", "", ErrInvalidIdentity
	}
	if decoded, decodeErr := base64.RawURLEncoding.DecodeString(encoded[1]); decodeErr != nil || len(decoded) != 32 {
		return "", "", ErrInvalidIdentity
	}
	return encoded[0], encoded[1], nil
}

func DigestCapability(requestID, capability string) ([sha256.Size]byte, error) {
	requestIDBytes, err := base64.RawURLEncoding.DecodeString(requestID)
	if err != nil || len(requestIDBytes) != 16 {
		return [sha256.Size]byte{}, ErrInvalidIdentity
	}
	capabilityBytes, err := base64.RawURLEncoding.DecodeString(capability)
	if err != nil || len(capabilityBytes) != 32 {
		return [sha256.Size]byte{}, ErrInvalidIdentity
	}
	return hashIdentity(requestIDBytes, capabilityBytes), nil
}

func hashIdentity(requestID, capability []byte) [sha256.Size]byte {
	material := make([]byte, 0, len(requestID)+1+len(capability))
	material = append(material, requestID...)
	material = append(material, 0)
	material = append(material, capability...)
	return sha256.Sum256(material)
}
