package directivefile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
)

func TestSourceReadsNestedDirectiveFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "team-a", "services")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"target":{"url":"https://api.example.com"}}`)
	if err := os.WriteFile(filepath.Join(path, "primary.json"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	source := New(Options{Root: root, MaxResponseBytes: 64 << 10})
	got, err := source.Read(t.Context(), directive.FileReference{Path: "team-a/services/primary.json"})
	if err != nil || string(got) != string(want) {
		t.Fatalf("unexpected file result: got=%s err=%v", got, err)
	}
}

func TestSourceClassifiesMissingFile(t *testing.T) {
	source := New(Options{Root: t.TempDir(), MaxResponseBytes: 64 << 10})
	_, err := source.Read(t.Context(), directive.FileReference{Path: "missing/directive.json"})
	if !errors.Is(err, directive.ErrRemoteNotFound) {
		t.Fatalf("unexpected missing file error: %v", err)
	}
}

func TestSourceRejectsInvalidOrNonRegularPath(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	source := New(Options{Root: root, MaxResponseBytes: 64 << 10})
	for _, path := range []string{"../outside.json", "/absolute.json", "directory"} {
		_, err := source.Read(t.Context(), directive.FileReference{Path: path})
		if !errors.Is(err, directive.ErrRemoteInvalid) {
			t.Fatalf("unexpected invalid path error for %q: %v", path, err)
		}
	}
}

func TestSourceRejectsSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{"target":{"url":"https://outside.example"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside.json")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	source := New(Options{Root: root, MaxResponseBytes: 64 << 10})
	if value, err := source.Read(t.Context(), directive.FileReference{Path: "outside.json"}); err == nil || len(value) != 0 {
		t.Fatalf("root escape succeeded: value=%s err=%v", value, err)
	}
}

func TestSourceRejectsOversizedFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "large.json"), []byte("123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := New(Options{Root: root, MaxResponseBytes: 8})
	_, err := source.Read(t.Context(), directive.FileReference{Path: "large.json"})
	if !errors.Is(err, directive.ErrRemoteInvalid) {
		t.Fatalf("unexpected oversized file error: %v", err)
	}
}

func TestSourceHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := New(Options{Root: t.TempDir(), MaxResponseBytes: 64 << 10})
	_, err := source.Read(ctx, directive.FileReference{Path: "directive.json"})
	if !errors.Is(err, directive.ErrRemoteUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation error: %v", err)
	}
}
