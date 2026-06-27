package eventbus

import (
	"context"
	"sync"
	"testing"
	"time"
)

type slowPublisher struct {
	release chan struct{}
}

func (p *slowPublisher) Publish(context.Context, Event) error {
	<-p.release
	return nil
}

func (p *slowPublisher) Close(context.Context) error {
	return nil
}

func TestAsyncPublisherCloseRejectsConcurrentPublishes(t *testing.T) {
	base := &slowPublisher{release: make(chan struct{})}
	publisher := NewAsyncPublisher(base, AsyncOptions{QueueSize: 1})
	if err := publisher.Publish(context.Background(), Event{Type: TypeUsage, EventID: "evt-1"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	closeStarted := make(chan struct{})
	closeDone := make(chan error, 1)
	go func() {
		close(closeStarted)
		closeDone <- publisher.Close(context.Background())
	}()
	<-closeStarted
	time.Sleep(10 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = publisher.Publish(context.Background(), Event{Type: TypeUsage, EventID: "evt-concurrent"})
		}()
	}
	wg.Wait()
	close(base.release)

	if err := <-closeDone; err != nil {
		t.Fatalf("close failed: %v", err)
	}
}
