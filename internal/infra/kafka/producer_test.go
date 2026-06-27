package kafka

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
)

func TestProducerProduceSyncRecreatesConfirmedMissingTopic(t *testing.T) {
	var produceCalls int
	var confirmCalls int
	var recreateCalls int

	p := &Producer{
		ensureTopic: true,
	}
	p.produceRecordFn = func(_ context.Context, _ string, _ string, _ []byte, done func(error)) {
		produceCalls++
		if produceCalls == 1 {
			done(kerr.UnknownTopicOrPartition)
			return
		}
		done(nil)
	}
	p.topicMissingFn = func(context.Context, string) (bool, error) {
		confirmCalls++
		return true, nil
	}
	p.ensureTopicExistsFn = func(context.Context, string) error {
		recreateCalls++
		return nil
	}

	if err := p.ProduceSync(context.Background(), "prod.proxy.capture", "req-1", []byte("payload")); err != nil {
		t.Fatalf("unexpected produce error: %v", err)
	}
	if produceCalls != 2 {
		t.Fatalf("unexpected produce calls: %d", produceCalls)
	}
	if confirmCalls != 1 {
		t.Fatalf("unexpected confirm calls: %d", confirmCalls)
	}
	if recreateCalls != 1 {
		t.Fatalf("unexpected recreate calls: %d", recreateCalls)
	}
}

func TestProducerProduceSyncSkipsRecreateWithoutConfirmedMissing(t *testing.T) {
	var recreateCalls int

	p := &Producer{
		ensureTopic: true,
	}
	p.produceRecordFn = func(_ context.Context, _ string, _ string, _ []byte, done func(error)) {
		done(kerr.UnknownTopicOrPartition)
	}
	p.topicMissingFn = func(context.Context, string) (bool, error) {
		return false, nil
	}
	p.ensureTopicExistsFn = func(context.Context, string) error {
		recreateCalls++
		return nil
	}

	err := p.ProduceSync(context.Background(), "prod.proxy.capture", "req-1", []byte("payload"))
	if !errors.Is(err, kerr.UnknownTopicOrPartition) {
		t.Fatalf("expected unknown topic error, got %v", err)
	}
	if recreateCalls != 0 {
		t.Fatalf("unexpected recreate calls: %d", recreateCalls)
	}
}

func TestProducerRecreateTopicIfConfirmedMissingIsSingleFlight(t *testing.T) {
	var recreateCalls int32

	p := &Producer{
		ensureTopic: true,
	}
	p.topicMissingFn = func(context.Context, string) (bool, error) {
		time.Sleep(50 * time.Millisecond)
		return true, nil
	}
	p.ensureTopicExistsFn = func(context.Context, string) error {
		atomic.AddInt32(&recreateCalls, 1)
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- p.recreateTopicIfConfirmedMissing("prod.proxy.capture")
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected recreate error: %v", err)
		}
	}
	if got := atomic.LoadInt32(&recreateCalls); got != 1 {
		t.Fatalf("unexpected recreate calls: %d", got)
	}
}
