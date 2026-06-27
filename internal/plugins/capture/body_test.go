package capture

import "testing"

func TestBuildBodyEncodesJSON(t *testing.T) {
	body := buildBody([]byte(`{"ok":true,"count":2}`), "application/json; charset=utf-8")

	if body.Encoding != BodyEncodingJSON {
		t.Fatalf("unexpected encoding: %s", body.Encoding)
	}
	content, ok := body.Content.(map[string]any)
	if !ok {
		t.Fatalf("expected JSON object content, got %#v", body.Content)
	}
	if content["ok"] != true || content["count"] != float64(2) {
		t.Fatalf("unexpected JSON content: %#v", content)
	}
	if body.Size != len(`{"ok":true,"count":2}`) {
		t.Fatalf("unexpected body size: %d", body.Size)
	}
}

func TestBuildBodyEncodesText(t *testing.T) {
	body := buildBody([]byte("hello"), "text/plain")

	if body.Encoding != BodyEncodingText || body.Content != "hello" {
		t.Fatalf("unexpected text body: %#v", body)
	}
}

func TestBuildBodyEncodesBase64(t *testing.T) {
	body := buildBody([]byte{0xff, 0x00, 0x01}, "application/octet-stream")

	if body.Encoding != BodyEncodingBase64 || body.Content != "/wAB" {
		t.Fatalf("unexpected base64 body: %#v", body)
	}
}

func TestBuildBodySniffsTextWhenContentTypeMissing(t *testing.T) {
	body := buildBody([]byte("hello"), "")

	if body.Encoding != BodyEncodingText || body.Content != "hello" {
		t.Fatalf("unexpected sniffed text body: %#v", body)
	}
}
