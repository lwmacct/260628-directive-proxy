package requestmeta

import "testing"

func TestNormalizeAndMatchMetadata(t *testing.T) {
	metadata, err := Normalize(map[string][]string{
		"x-dproxy-request-id": {"request-2", "request-1", "request-1"},
		"X-Dproxy-Tenant":     {"tenant-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := metadata["X-Dproxy-Request-Id"]; len(got) != 2 || got[0] != "request-1" || got[1] != "request-2" {
		t.Fatalf("unexpected normalized values: %#v", metadata)
	}
	selector, err := NormalizeSelector(map[string]string{
		"X-DPROXY-REQUEST-ID": "request-2",
		"x-dproxy-tenant":     "tenant-a",
	})
	if err != nil || !Matches(metadata, selector) {
		t.Fatalf("metadata did not match: selector=%#v err=%v", selector, err)
	}
}

func TestMetadataRejectsReservedTraceIDAndInvalidSelector(t *testing.T) {
	for _, selector := range []map[string]string{
		nil,
		{},
		{"X-Test": "value"},
		{"X-Dproxy-Trace-ID": "forged"},
		{"X-Dproxy-Key": " padded "},
	} {
		if _, err := NormalizeSelector(selector); err == nil {
			t.Fatalf("expected invalid selector: %#v", selector)
		}
	}
}

func TestApplyMetadataOperations(t *testing.T) {
	metadata := Metadata{}
	for _, operation := range []struct {
		action string
		name   string
		values []string
	}{
		{action: "=", name: "X-Dproxy-Key", values: []string{"one"}},
		{action: "+", name: "x-dproxy-key", values: []string{"two"}},
	} {
		if err := Apply(metadata, operation.action, operation.name, operation.values); err != nil {
			t.Fatal(err)
		}
	}
	if got := metadata["X-Dproxy-Key"]; len(got) != 2 {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	if err := Apply(metadata, "-", "X-Dproxy-Key", nil); err != nil || len(metadata) != 0 {
		t.Fatalf("metadata remove failed: metadata=%#v err=%v", metadata, err)
	}
}
