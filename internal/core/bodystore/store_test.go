package bodystore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	vmmetrics "github.com/VictoriaMetrics/metrics"
)

func testController() *Controller {
	return New(Config{MemoryMaxBytes: 12, MaxBodyBytes: 8, ChunkBytes: 4, QueueMaxRequests: 2})
}

func TestStoreStreamsAndReplaysInMemory(t *testing.T) {
	controller := testController()
	store, err := controller.Stream(t.Context(), io.NopCloser(strings.NewReader("payload")), 7, Observer{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	firstBody, firstErr := io.ReadAll(first)
	secondBody, secondErr := io.ReadAll(second)
	if firstErr != nil || secondErr != nil || string(firstBody) != "payload" || string(secondBody) != "payload" {
		t.Fatalf("unexpected replay: first=%q/%v second=%q/%v", firstBody, firstErr, secondBody, secondErr)
	}
	store.Retire()
	if snapshot := controller.Snapshot(); snapshot.MemoryUsedBytes != 0 || snapshot.QueuedRequests != 0 {
		t.Fatalf("capacity leaked: %#v", snapshot)
	}
}

func TestStoreQueuesUntilReservationReleased(t *testing.T) {
	controller := New(Config{MemoryMaxBytes: 8, MaxBodyBytes: 4, ChunkBytes: 4, QueueMaxRequests: 1})
	first, err := controller.Stream(t.Context(), io.NopCloser(strings.NewReader("1234")), 4, Observer{})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		queued, err := controller.Stream(t.Context(), io.NopCloser(strings.NewReader("5678")), 4, Observer{}, StreamOptions{QueueWait: time.Second})
		if err == nil {
			queued.Retire()
		}
		result <- err
	}()
	<-started
	deadline := time.Now().Add(time.Second)
	for controller.Snapshot().QueuedRequests != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if controller.Snapshot().QueuedRequests != 1 {
		t.Fatal("request did not enter queue")
	}
	first.Retire()
	if err := <-result; err != nil {
		t.Fatalf("queued request was not admitted: %v", err)
	}
}

func TestStoreQueueTimeoutAndCancellation(t *testing.T) {
	controller := New(Config{MemoryMaxBytes: 8, MaxBodyBytes: 4, ChunkBytes: 4, QueueMaxRequests: 1})
	first, err := controller.Stream(t.Context(), io.NopCloser(strings.NewReader("1234")), 4, Observer{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = controller.Stream(t.Context(), io.NopCloser(strings.NewReader("5678")), 4, Observer{}, StreamOptions{QueueWait: time.Millisecond})
	if !errors.Is(err, ErrQueueTimeout) {
		t.Fatalf("unexpected timeout error: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = controller.Stream(ctx, io.NopCloser(strings.NewReader("5678")), 4, Observer{}, StreamOptions{QueueWait: time.Second})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation error: %v", err)
	}
	first.Retire()
}

func TestStoreQueueFull(t *testing.T) {
	controller := New(Config{MemoryMaxBytes: 8, MaxBodyBytes: 4, ChunkBytes: 4, QueueMaxRequests: 1})
	first, err := controller.Stream(t.Context(), io.NopCloser(strings.NewReader("1234")), 4, Observer{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Retire()
	queuedCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		_, _ = controller.Stream(queuedCtx, io.NopCloser(strings.NewReader("5678")), 4, Observer{}, StreamOptions{QueueWait: time.Second})
	}()
	deadline := time.Now().Add(time.Second)
	for controller.Snapshot().QueuedRequests != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := controller.Stream(t.Context(), io.NopCloser(strings.NewReader("90ab")), 4, Observer{}, StreamOptions{QueueWait: time.Second}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("unexpected queue-full error: %v", err)
	}
	cancel()
}

func TestStoreRejectsOversizedBodyBeforeReading(t *testing.T) {
	controller := testController()
	if _, err := controller.Stream(t.Context(), io.NopCloser(strings.NewReader("123456789")), 9, Observer{}); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStoreRejectsUnknownBodyThatCannotFitInstanceMemory(t *testing.T) {
	controller := New(Config{MemoryMaxBytes: 4, MaxBodyBytes: 8, QueueMaxRequests: 1})
	if _, err := controller.Stream(t.Context(), io.NopCloser(strings.NewReader("123")), -1, Observer{}); !errors.Is(err, ErrStoreCapacity) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestControllerMetricsExposeAdmissionOutcomesAndWait(t *testing.T) {
	controller := New(Config{MemoryMaxBytes: 8, MaxBodyBytes: 4, ChunkBytes: 4, QueueMaxRequests: 1})
	set := vmmetrics.NewSet()
	controller.RegisterMetrics(set, "directive_proxy_")
	first, err := controller.Admit(t.Context(), 4, 4, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	if _, err := controller.Admit(t.Context(), 4, 4, time.Millisecond, 4); !errors.Is(err, ErrQueueTimeout) {
		t.Fatalf("unexpected queue timeout: %v", err)
	}
	queuedContext, cancelQueued := context.WithCancel(t.Context())
	defer cancelQueued()
	queuedResult := make(chan error, 1)
	go func() {
		_, queueErr := controller.Admit(queuedContext, 4, 4, time.Second, 4)
		queuedResult <- queueErr
	}()
	deadline := time.Now().Add(time.Second)
	for controller.Snapshot().QueuedRequests != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := controller.Admit(t.Context(), 4, 4, time.Second, 4); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("unexpected queue full error: %v", err)
	}
	cancelQueued()
	if err := <-queuedResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected queued cancellation: %v", err)
	}
	if _, err := controller.Admit(t.Context(), 8, 8, 0, 4); !errors.Is(err, ErrStoreCapacity) {
		t.Fatalf("unexpected capacity error: %v", err)
	}

	var output bytes.Buffer
	set.WritePrometheus(&output)
	metrics := output.String()
	for _, metric := range []string{
		"directive_proxy_body_store_admitted_total 1",
		"directive_proxy_body_store_queue_full_total 1",
		"directive_proxy_body_store_queue_timeout_total 1",
		"directive_proxy_body_store_canceled_total 1",
		"directive_proxy_body_store_capacity_rejected_total 1",
		"directive_proxy_body_store_admission_wait_seconds_count 3",
	} {
		if !strings.Contains(metrics, metric) {
			t.Fatalf("metrics output is missing %q: %s", metric, metrics)
		}
	}
}
