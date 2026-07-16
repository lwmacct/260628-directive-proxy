package retry

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const IDHeader = "Dproxy-Retry-ID"

var ErrInvalidIdentity = errors.New("invalid retry identity")

type Identity struct {
	id     string
	digest [sha256.Size]byte
	valid  bool
}

func TakeIdentity(req *http.Request) (Identity, error) {
	if req == nil {
		return Identity{}, nil
	}
	raw := strings.TrimSpace(req.Header.Get(IDHeader))
	req.Header.Del(IDHeader)
	if raw == "" {
		return Identity{}, nil
	}
	id, err := ParseID(raw)
	if err != nil {
		return Identity{}, ErrInvalidIdentity
	}
	return Identity{id: id.String(), digest: sha256.Sum256(id[:]), valid: true}, nil
}

func ParseID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != raw {
		return uuid.Nil, ErrInvalidIdentity
	}
	return id, nil
}

func IDDigest(raw string) ([sha256.Size]byte, error) {
	id, err := ParseID(raw)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(id[:]), nil
}

func (identity Identity) String() string            { return identity.id }
func (identity Identity) Valid() bool               { return identity.valid && identity.id != "" }
func (identity Identity) Digest() [sha256.Size]byte { return identity.digest }
