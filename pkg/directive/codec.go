package directive

import (
	"bytes"
	"encoding/json"
	"io"
)

func DecodePayload(raw []byte) (Payload, error) {
	payload, err := decodeStrict[Payload](raw)
	if err != nil {
		return Payload{}, err
	}
	return NormalizePayload(payload)
}

// DecodeTemplate decodes a Payload fragment that deliberately omits target.
// Templates are suitable for control-plane policy storage and must be completed
// with a target before they can be consumed by the data plane.
func DecodeTemplate(raw []byte) (Payload, error) {
	canonical, err := CanonicalTemplate(raw)
	if err != nil {
		return Payload{}, err
	}
	return decodeStrict[Payload](canonical)
}

func CanonicalTemplate(raw []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, ErrInvalidPayload
	}
	if _, exists := fields["target"]; exists {
		return nil, ErrInvalidPayload
	}
	payload, err := decodeStrict[Payload](raw)
	if err != nil {
		return nil, err
	}
	payload, err = normalizePayload(payload, false)
	if err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrInvalidPayload
	}
	if err = json.Unmarshal(canonical, &fields); err != nil {
		return nil, ErrInvalidPayload
	}
	delete(fields, "target")
	return json.Marshal(fields)
}

func DecodeRemoteSpec(raw []byte) (RemoteSpec, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return RemoteSpec{}, ErrInvalidPayload
	}
	backends := 0
	for _, name := range []string{"http", "redis", "file"} {
		if _, exists := fields[name]; exists {
			backends++
		}
	}
	if backends != 1 {
		return RemoteSpec{}, ErrInvalidPayload
	}
	spec, err := decodeStrict[RemoteSpec](raw)
	if err != nil {
		return RemoteSpec{}, err
	}
	return NormalizeRemoteSpec(spec)
}

func (target *TargetSection) UnmarshalJSON(raw []byte) error {
	type plainTarget TargetSection
	decoded, err := decodeStrict[plainTarget](raw)
	if err != nil {
		return ErrInvalidPayload
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil || len(fields) != 1 {
		return ErrInvalidPayload
	}
	*target = TargetSection(decoded)
	return nil
}

func decodeStrict[T any](raw []byte) (T, error) {
	var value T
	if len(raw) == 0 {
		return value, ErrInvalidPayload
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, ErrInvalidPayload
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return value, ErrInvalidPayload
	}
	return value, nil
}
