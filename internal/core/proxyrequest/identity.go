package proxyrequest

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const RetryIDHeader = "Dproxy-Retry-ID"

var ErrInvalidIdentity = errors.New("invalid proxy request retry identity")

type Identity struct {
	RetryID string
	digest  [sha256.Size]byte
	valid   bool
}

func TakeIdentity(req *http.Request) (Identity, error) {
	if req == nil {
		return Identity{}, nil
	}
	raw := strings.TrimSpace(req.Header.Get(RetryIDHeader))
	req.Header.Del(RetryIDHeader)
	if raw == "" {
		return Identity{}, nil
	}
	id, err := ParseRetryID(raw)
	if err != nil {
		return Identity{}, ErrInvalidIdentity
	}
	return Identity{RetryID: id.String(), digest: sha256.Sum256(id[:]), valid: true}, nil
}

func ParseRetryID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != raw {
		return uuid.Nil, ErrInvalidIdentity
	}
	return id, nil
}

func (i Identity) Valid() bool               { return i.valid && i.RetryID != "" }
func (i Identity) Digest() [sha256.Size]byte { return i.digest }
func (i Identity) HasRetryID() bool          { return i.Valid() }

func RetryIDDigest(raw string) ([sha256.Size]byte, error) {
	id, err := ParseRetryID(raw)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(id[:]), nil
}
