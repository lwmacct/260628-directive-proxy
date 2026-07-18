package metadata

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCompileReturnsImmutableDirectiveMetadata(t *testing.T) {
	fields, err := Compile(map[string]string{KeyUserID: "user-1", KeyUserKey: "key-1", "tenant_id": "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	if fields.UserID() != "user-1" || fields.UserKey() != "key-1" || fields.Get("tenant_id") != "tenant-a" {
		t.Fatalf("unexpected metadata: %#v", fields.Map())
	}
	copy := fields.Map()
	copy[KeyUserKey] = "mutated"
	if fields.UserKey() != "key-1" {
		t.Fatal("metadata exposed mutable state")
	}
}

func TestCompileRejectsInvalidDirectiveMetadata(t *testing.T) {
	for _, input := range []map[string]string{
		{reservedTraceID: "forged"},
		{KeyUserKey: " padded "},
		{KeyUserKey: "key-1", "Tenant": "tenant-a"},
		{KeyUserKey: "key-1", "bad-key": "value"},
	} {
		if _, err := Compile(input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid metadata: %#v err=%v", input, err)
		}
	}
}

func TestCompileAllowsEmptyDirectiveMetadata(t *testing.T) {
	fields, err := Compile(nil)
	if err != nil {
		t.Fatal(err)
	}
	result := fields.Map()
	if result == nil || len(result) != 0 {
		t.Fatalf("unexpected empty metadata: %#v", result)
	}
}

func TestCompileAllowsFullFieldCapacity(t *testing.T) {
	fields := make(map[string]string, MaxFields)
	for index := range MaxFields {
		fields[fmt.Sprintf("field_%02d", index)] = "value"
	}
	if _, err := Compile(fields); err != nil {
		t.Fatalf("compile metadata at field limit: %v", err)
	}
	fields["overflow"] = "value"
	if _, err := Compile(fields); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected field overflow to reject metadata: %v", err)
	}
}

func TestCompileAllowsFullByteCapacity(t *testing.T) {
	fields := make(map[string]string, 15)
	for index := range 14 {
		prefix := fmt.Sprintf("field_%02d_", index)
		fields[prefix+strings.Repeat("a", MaxNameBytes-len(prefix))] = strings.Repeat("x", MaxValueBytes)
	}
	fields["overflow"] = strings.Repeat("y", 120)
	if _, err := Compile(fields); err != nil {
		t.Fatalf("compile metadata at byte limit: %v", err)
	}
	fields["overflow"] += "y"
	if _, err := Compile(fields); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected byte overflow to reject metadata: %v", err)
	}
}
