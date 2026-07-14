package requestmeta

import (
	"errors"
	"net/http"
	"sort"
	"strings"
)

const (
	Prefix            = "x-dproxy-"
	ReservedTraceID   = "X-Dproxy-Trace-ID"
	MaxFields         = 16
	MaxValuesPerField = 8
	MaxNameBytes      = 128
	MaxValueBytes     = 512
	MaxTotalBytes     = 8 << 10
)

var ErrInvalid = errors.New("invalid proxy request metadata")

type Metadata map[string][]string

type Selector map[string]string

func IsName(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), Prefix)
}

func Apply(metadata Metadata, action, rawName string, rawValues []string) error {
	name, err := normalizeName(rawName)
	if err != nil {
		return err
	}
	values := make([]string, len(rawValues))
	for i, value := range rawValues {
		if err := validateValue(value); err != nil {
			return err
		}
		values[i] = value
	}
	switch action {
	case "=":
		metadata[name] = values
	case "+":
		metadata[name] = append(metadata[name], values...)
	case "-":
		delete(metadata, name)
	default:
		return ErrInvalid
	}
	return Validate(metadata)
}

func Normalize(in map[string][]string) (Metadata, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(Metadata, len(in))
	for rawName, rawValues := range in {
		name, err := normalizeName(rawName)
		if err != nil {
			return nil, err
		}
		values := append([]string(nil), rawValues...)
		for _, value := range values {
			if err := validateValue(value); err != nil {
				return nil, err
			}
		}
		sort.Strings(values)
		values = compact(values)
		out[name] = append(out[name], values...)
	}
	for name, values := range out {
		sort.Strings(values)
		out[name] = compact(values)
	}
	if err := Validate(out); err != nil {
		return nil, err
	}
	return out, nil
}

func NormalizeSelector(in map[string]string) (Selector, error) {
	if len(in) == 0 {
		return nil, ErrInvalid
	}
	out := make(Selector, len(in))
	for rawName, value := range in {
		name, err := normalizeName(rawName)
		if err != nil || validateValue(value) != nil {
			return nil, ErrInvalid
		}
		out[name] = value
	}
	if len(out) > MaxFields {
		return nil, ErrInvalid
	}
	return out, nil
}

func Validate(metadata Metadata) error {
	if len(metadata) > MaxFields {
		return ErrInvalid
	}
	total := 0
	for rawName, values := range metadata {
		name, err := normalizeName(rawName)
		if err != nil || name != rawName || len(values) > MaxValuesPerField {
			return ErrInvalid
		}
		total += len(name)
		for _, value := range values {
			if err := validateValue(value); err != nil {
				return err
			}
			total += len(value)
		}
	}
	if total > MaxTotalBytes {
		return ErrInvalid
	}
	return nil
}

func Clone(in Metadata) Metadata {
	if len(in) == 0 {
		return nil
	}
	out := make(Metadata, len(in))
	for name, values := range in {
		out[name] = append([]string(nil), values...)
	}
	return out
}

func Equal(a, b Metadata) bool {
	if len(a) != len(b) {
		return false
	}
	for name, values := range a {
		other, ok := b[name]
		if !ok || len(values) != len(other) {
			return false
		}
		for i := range values {
			if values[i] != other[i] {
				return false
			}
		}
	}
	return true
}

func Matches(metadata Metadata, selector Selector) bool {
	if len(selector) == 0 {
		return false
	}
	for name, expected := range selector {
		matched := false
		for _, value := range metadata[name] {
			if value == expected {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || !IsName(name) || len(name) > MaxNameBytes || strings.EqualFold(name, ReservedTraceID) {
		return "", ErrInvalid
	}
	canonical := http.CanonicalHeaderKey(name)
	if canonical == "" {
		return "", ErrInvalid
	}
	return canonical, nil
}

func validateValue(value string) error {
	if value == "" || value != strings.TrimSpace(value) || len(value) > MaxValueBytes || strings.ContainsAny(value, "\r\n") {
		return ErrInvalid
	}
	return nil
}

func compact(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
