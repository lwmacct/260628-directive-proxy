package bodymemory

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestControllerQueuesInFIFOOrderWithoutOvercommitting(t *testing.T) {
	controller := New(Config{MaxActiveBytes: 10, MaxBodyBytes: 10, QueueMax: 2, QueueWait: time.Second})
	first, err := controller.Reserve(t.Context(), 8)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		name        string
		reservation *Reservation
		err         error
	}
	results := make(chan result, 2)
	for _, item := range []struct {
		name string
		size int64
	}{{"second", 9}, {"third", 2}} {
		go func() {
			reservation, reserveErr := controller.Reserve(context.Background(), item.size)
			results <- result{name: item.name, reservation: reservation, err: reserveErr}
		}()
		wantQueued := 1
		if item.name == "third" {
			wantQueued = 2
		}
		for controller.Snapshot().QueuedRequests != wantQueued {
			time.Sleep(time.Millisecond)
		}
	}
	if snapshot := controller.Snapshot(); snapshot.UsedBytes != 8 || snapshot.QueuedRequests != 2 {
		t.Fatalf("unexpected queued snapshot: %#v", snapshot)
	}
	first.Close()
	second := <-results
	if second.err != nil || second.name != "second" {
		t.Fatalf("FIFO order was not preserved: %#v", second)
	}
	if snapshot := controller.Snapshot(); snapshot.UsedBytes != 9 || snapshot.QueuedRequests != 1 {
		t.Fatalf("unexpected dispatched snapshot: %#v", snapshot)
	}
	second.reservation.Close()
	third := <-results
	third.reservation.Close()
}

func TestControllerCancelsQueuedReservation(t *testing.T) {
	controller := New(Config{MaxActiveBytes: 4, MaxBodyBytes: 4, QueueMax: 1})
	active, err := controller.Reserve(t.Context(), 4)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, reserveErr := controller.Reserve(ctx, 1)
		done <- reserveErr
	}()
	for controller.Snapshot().QueuedRequests == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation: %v", err)
	}
	active.Close()
	if snapshot := controller.Snapshot(); snapshot.UsedBytes != 0 || snapshot.QueuedRequests != 0 {
		t.Fatalf("reservation leaked: %#v", snapshot)
	}
}

func TestControllerReportsQueueFullAndWaitTimeout(t *testing.T) {
	controller := New(Config{MaxActiveBytes: 4, MaxBodyBytes: 4, QueueMax: 1, QueueWait: 20 * time.Millisecond})
	active, err := controller.Reserve(t.Context(), 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(active.Close)

	timedOut := make(chan error, 1)
	go func() {
		_, reserveErr := controller.Reserve(context.Background(), 1)
		timedOut <- reserveErr
	}()
	for controller.Snapshot().QueuedRequests == 0 {
		time.Sleep(time.Millisecond)
	}
	if _, err := controller.Reserve(t.Context(), 1); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("unexpected queue-full result: %v", err)
	}
	if err := <-timedOut; !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("unexpected wait result: %v", err)
	}
	if snapshot := controller.Snapshot(); snapshot.UsedBytes != 4 || snapshot.QueuedRequests != 0 {
		t.Fatalf("timed-out waiter leaked: %#v", snapshot)
	}
}

func TestBodyKeepsReservationUntilLastLeaseCloses(t *testing.T) {
	controller := New(Config{MaxActiveBytes: 16, MaxBodyBytes: 16})
	reservation, err := controller.Reserve(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	body := NewBody([]byte("payload"), reservation)
	lease := body.Acquire()
	body.Release()
	if snapshot := controller.Snapshot(); snapshot.UsedBytes != 7 {
		t.Fatalf("body released while lease was active: %#v", snapshot)
	}
	if got := string(lease.Bytes()); got != "payload" {
		t.Fatalf("unexpected body: %q", got)
	}
	if digest := lease.Digest(); digest == [32]byte{} {
		t.Fatal("body digest was not computed")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if snapshot := controller.Snapshot(); snapshot.UsedBytes != 0 {
		t.Fatalf("body reservation was not released: %#v", snapshot)
	}
}
