package metadata

import (
	"errors"
	"strings"
)

const (
	KeyUserID  = "user_id"
	KeyUserKey = "user_key"

	MaxFields     = 16
	MaxNameBytes  = 64
	MaxValueBytes = 512
	MaxTotalBytes = 8 << 10
)

var ErrInvalid = errors.New("invalid directive metadata")

// Set is an immutable collection of directive-defined correlation fields.
type Set struct {
	fields map[string]string
}

func Compile(input map[string]string) (Set, error) {
	if len(input) > MaxFields {
		return Set{}, ErrInvalid
	}
	fields := make(map[string]string, len(input))
	total := 0
	for key, value := range input {
		if !validKey(key) || !validValue(value) {
			return Set{}, ErrInvalid
		}
		total += len(key) + len(value)
		if total > MaxTotalBytes {
			return Set{}, ErrInvalid
		}
		fields[key] = value
	}
	return Set{fields: fields}, nil
}

func (set Set) UserID() string {
	return set.fields[KeyUserID]
}

func (set Set) UserKey() string {
	return set.fields[KeyUserKey]
}

func (set Set) Get(key string) string {
	return set.fields[key]
}

func (set Set) Map() map[string]string {
	out := make(map[string]string, len(set.fields))
	for key, value := range set.fields {
		out[key] = value
	}
	return out
}

func validKey(key string) bool {
	if key == "" || len(key) > MaxNameBytes || key != strings.TrimSpace(key) {
		return false
	}
	for index, char := range key {
		if char >= 'a' && char <= 'z' || index > 0 && char >= '0' && char <= '9' || index > 0 && char == '_' {
			continue
		}
		return false
	}
	return true
}

func validValue(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= MaxValueBytes && !strings.ContainsAny(value, "\r\n")
}
