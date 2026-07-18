package metadata

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCompileAndInjectTraceID(t *testing.T) {
	directive, err := Compile(map[string]string{KeyUserID: "user-1", KeyUserKey: "key-1", "tenant_id": "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	effective, err := directive.WithTraceID("019f-trace")
	if err != nil {
		t.Fatal(err)
	}
	if effective.UserID() != "user-1" || effective.UserKey() != "key-1" || effective.TraceID() != "019f-trace" || effective.Get("tenant_id") != "tenant-a" {
		t.Fatalf("unexpected metadata: %#v", effective.Map())
	}
	copy := effective.Map()
	copy[KeyUserKey] = "mutated"
	if effective.UserKey() != "key-1" {
		t.Fatal("metadata exposed mutable state")
	}
}

func TestCompileRejectsInvalidDirectiveMetadata(t *testing.T) {
	for _, input := range []map[string]string{
		{KeyTraceID: "forged"},
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
	directive, err := Compile(nil)
	if err != nil {
		t.Fatal(err)
	}
	effective, err := directive.WithTraceID("019f-trace")
	if err != nil {
		t.Fatal(err)
	}
	if len(effective.Map()) != 1 || effective.TraceID() != "019f-trace" {
		t.Fatalf("unexpected system-only metadata: %#v", effective.Map())
	}
}

func TestCompileReservesTraceIDCapacity(t *testing.T) {
	fields := map[string]string{KeyUserKey: "key"}
	for index := 1; index < MaxDirectiveFields; index++ {
		prefix := fmt.Sprintf("field_%02d_", index)
		fields[prefix+strings.Repeat("a", MaxNameBytes-len(prefix))] = strings.Repeat("x", MaxValueBytes)
	}
	if _, err := Compile(fields); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected reserved trace bytes to reject directive metadata: %v", err)
	}

	delete(fields, "field_14_"+strings.Repeat("a", MaxNameBytes-len("field_14_")))
	if _, err := Compile(fields); err != nil {
		t.Fatalf("compile metadata within reserved capacity: %v", err)
	}
	fields["overflow"] = "value"
	fields["overflow_2"] = "value"
	if _, err := Compile(fields); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected reserved trace field to reject directive metadata: %v", err)
	}
}

func TestWithTraceIDChecksMergedSize(t *testing.T) {
	set := Set{fields: map[string]string{
		KeyUserKey: "key",
		"data":     strings.Repeat("x", MaxTotalBytes-len(KeyUserKey)-len("key")-len("data")-len(KeyTraceID)),
	}}
	if _, err := set.WithTraceID("x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected merged metadata byte overflow: %v", err)
	}
}
