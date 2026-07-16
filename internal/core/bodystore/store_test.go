package bodystore

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func testController(t *testing.T, memoryPerBody int64) *Controller {
	t.Helper()
	return New(Config{
		MemoryMaxBytes:     64,
		MemoryPerBodyBytes: memoryPerBody,
		DiskMaxBytes:       1024,
		MaxBodyBytes:       256,
		ChunkBytes:         4,
		TempDir:            t.TempDir(),
	})
}

func TestStoreStreamsToFirstReaderAndReplaysToSecondReader(t *testing.T) {
	controller := testController(t, 64)
	source, writer := io.Pipe()
	store, err := controller.Stream(t.Context(), source, -1, Observer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	first, err := store.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	written := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _ = writer.Write([]byte("first"))
		close(written)
		<-release
		_, _ = writer.Write([]byte("-second"))
		_ = writer.Close()
	}()

	prefix := make([]byte, len("first"))
	if _, err := io.ReadFull(first, prefix); err != nil {
		t.Fatal(err)
	}
	if string(prefix) != "first" {
		t.Fatalf("first reader did not receive live prefix: %q", prefix)
	}
	<-written
	second, err := store.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	firstTail, err := io.ReadAll(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBody, err := io.ReadAll(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstTail) != "-second" || string(secondBody) != "first-second" {
		t.Fatalf("unexpected replay: first_tail=%q second=%q", firstTail, secondBody)
	}
}

func TestStoreSpillsToAnonymousFileAndReleasesCapacity(t *testing.T) {
	tempDir := t.TempDir()
	controller := New(Config{
		MemoryMaxBytes: 64, MemoryPerBodyBytes: 4, DiskMaxBytes: 64,
		MaxBodyBytes: 64, ChunkBytes: 4, TempDir: tempDir,
	})
	store, err := controller.Stream(t.Context(), io.NopCloser(strings.NewReader("payload")), 7, Observer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	reader, err := store.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "payload" {
		t.Fatalf("unexpected spill replay: body=%q err=%v", body, err)
	}
	snapshot := controller.Snapshot()
	if snapshot.MemoryUsedBytes != 0 || snapshot.DiskUsedBytes != 7 {
		t.Fatalf("unexpected active capacity: %#v", snapshot)
	}
	store.Retire()
	if snapshot = controller.Snapshot(); snapshot.MemoryUsedBytes != 0 || snapshot.DiskUsedBytes != 0 {
		t.Fatalf("capacity was not released: %#v", snapshot)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("spill file remained visible: %#v", entries)
	}
}

func TestStoreEnforcesActualBodyLimitWithoutContentLength(t *testing.T) {
	controller := New(Config{
		MemoryMaxBytes: 8, MemoryPerBodyBytes: 8, DiskMaxBytes: 16,
		MaxBodyBytes: 4, ChunkBytes: 2, TempDir: t.TempDir(),
	})
	store, err := controller.Stream(t.Context(), io.NopCloser(strings.NewReader("12345")), -1, Observer{})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := store.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(reader)
	if !errors.Is(readErr, ErrBodyTooLarge) || string(body) != "1234" {
		t.Fatalf("unexpected limit result: body=%q err=%v", body, readErr)
	}
	result, err := store.Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete || !errors.Is(result.Err, ErrBodyTooLarge) {
		t.Fatalf("unexpected terminal result: %#v", result)
	}
	store.Retire()
	if snapshot := controller.Snapshot(); snapshot.MemoryUsedBytes != 0 || snapshot.DiskUsedBytes != 0 {
		t.Fatalf("capacity leaked: %#v", snapshot)
	}
}

func TestStoreRejectsOversizedDeclaredBodyBeforeReading(t *testing.T) {
	controller := New(Config{MaxBodyBytes: 4})
	if _, err := controller.Stream(t.Context(), io.NopCloser(strings.NewReader("12345")), 5, Observer{}); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("unexpected declaration result: %v", err)
	}
}

func TestStoreReportsSpillCapacityExhaustion(t *testing.T) {
	controller := New(Config{
		MemoryMaxBytes: 4, MemoryPerBodyBytes: 4, DiskMaxBytes: 4,
		MaxBodyBytes: 8, ChunkBytes: 4, TempDir: t.TempDir(),
	})
	store, err := controller.Stream(t.Context(), io.NopCloser(strings.NewReader("12345")), -1, Observer{})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := store.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(reader)
	if !errors.Is(readErr, ErrStoreCapacity) || string(body) != "1234" {
		t.Fatalf("unexpected capacity result: body=%q err=%v", body, readErr)
	}
	store.Retire()
	if snapshot := controller.Snapshot(); snapshot.MemoryUsedBytes != 0 || snapshot.DiskUsedBytes != 0 {
		t.Fatalf("capacity leaked after spill failure: %#v", snapshot)
	}
}

func TestReaderCancellationDoesNotStopIngestForFutureRetry(t *testing.T) {
	controller := testController(t, 64)
	source, writer := io.Pipe()
	store, err := controller.Stream(t.Context(), source, -1, Observer{})
	if err != nil {
		t.Fatal(err)
	}
	readerCtx, cancelReader := context.WithCancel(t.Context())
	first, err := store.Open(readerCtx)
	if err != nil {
		t.Fatal(err)
	}
	readResult := make(chan error, 1)
	go func() {
		_, readErr := first.Read(make([]byte, 1))
		readResult <- readErr
	}()
	cancelReader()
	if err := <-readResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected canceled reader result: %v", err)
	}
	go func() {
		_, _ = writer.Write([]byte("retry"))
		_ = writer.Close()
	}()
	second, err := store.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(second)
	if err != nil || string(body) != "retry" {
		t.Fatalf("ingest did not survive attempt cancellation: body=%q err=%v", body, err)
	}
	store.Retire()
}
