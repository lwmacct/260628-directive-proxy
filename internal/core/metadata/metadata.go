package metadata

import (
	"errors"
	"strings"
)

const (
	KeyUserID  = "user_id"
	KeyUserKey = "user_key"
	KeyTraceID = "trace_id"

	MaxFields     = 16
	MaxNameBytes  = 64
	MaxValueBytes = 512
	MaxTotalBytes = 8 << 10

	// Directive input reserves capacity for the Exchange-owned trace ID.
	MaxDirectiveFields     = MaxFields - 1
	MaxDirectiveTotalBytes = MaxTotalBytes - len(KeyTraceID) - MaxValueBytes
)

var ErrInvalid = errors.New("invalid directive metadata")

// Set is an immutable collection of correlation fields. Directive input cannot
// provide trace_id; Exchange adds that system-owned field later.
type Set struct {
	fields map[string]string
}

func Compile(input map[string]string) (Set, error) {
	if len(input) > MaxDirectiveFields {
		return Set{}, ErrInvalid
	}
	fields := make(map[string]string, len(input))
	total := 0
	for key, value := range input {
		if !validKey(key) || key == KeyTraceID || !validValue(value) {
			return Set{}, ErrInvalid
		}
		total += len(key) + len(value)
		if total > MaxDirectiveTotalBytes {
			return Set{}, ErrInvalid
		}
		fields[key] = value
	}
	return Set{fields: fields}, nil
}

func (set Set) WithTraceID(traceID string) (Set, error) {
	if !validValue(traceID) {
		return Set{}, ErrInvalid
	}
	if existing := set.fields[KeyTraceID]; existing != "" && existing != traceID {
		return Set{}, ErrInvalid
	}
	fields := set.Map()
	if fields == nil {
		fields = make(map[string]string, 1)
	}
	if len(fields) >= MaxFields && fields[KeyTraceID] == "" {
		return Set{}, ErrInvalid
	}
	fields[KeyTraceID] = traceID
	total := 0
	for key, value := range fields {
		total += len(key) + len(value)
	}
	if total > MaxTotalBytes {
		return Set{}, ErrInvalid
	}
	return Set{fields: fields}, nil
}

func (set Set) UserID() string {
	return set.fields[KeyUserID]
}

func (set Set) UserKey() string {
	return set.fields[KeyUserKey]
}

func (set Set) TraceID() string {
	return set.fields[KeyTraceID]
}

func (set Set) Get(key string) string {
	return set.fields[key]
}

func (set Set) Map() map[string]string {
	if len(set.fields) == 0 {
		return nil
	}
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
