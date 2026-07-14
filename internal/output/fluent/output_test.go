package fluentoutput

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/tinylib/msgp/msgp"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

func TestOutputSendsAcknowledgedForwardRecord(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := listener.Close(); closeErr != nil {
			t.Errorf("close Fluent listener: %v", closeErr)
		}
	})
	received := make(chan []any, 1)
	errors := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			errors <- acceptErr
			return
		}
		defer func() { _ = conn.Close() }()
		value, readErr := msgp.NewReader(conn).ReadIntf()
		if readErr != nil {
			errors <- readErr
			return
		}
		message, ok := value.([]any)
		if !ok || len(message) != 4 {
			errors <- &unexpectedForwardMessage{value: value}
			return
		}
		options, ok := message[3].(map[string]any)
		if !ok {
			errors <- &unexpectedForwardMessage{value: message[3]}
			return
		}
		chunk, _ := options["chunk"].(string)
		ack := msgp.AppendMapHeader(nil, 1)
		ack = msgp.AppendString(ack, "ack")
		ack = msgp.AppendString(ack, chunk)
		if _, writeErr := conn.Write(ack); writeErr != nil {
			errors <- writeErr
			return
		}
		received <- message
	}()

	host, portValue, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portValue)
	output := New(Config{
		Name: "fluent-test", Endpoint: "tcp://" + net.JoinHostPort(host, strconv.Itoa(port)),
		Connections: 1, ClientQueueCapacity: 16, ConnectTimeout: time.Second, HandshakeTimeout: time.Second,
		WriteTimeout: time.Second, ACKTimeout: time.Second, RetryMaxAttempts: 1,
		RetryMinBackoff: time.Millisecond, RetryMaxBackoff: time.Millisecond, TagPrefix: "dproxy", DeliveryAtLeastOnce: true,
	})
	if err := output.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := output.Close(context.Background()); closeErr != nil {
			t.Errorf("close Fluent output: %v", closeErr)
		}
	})
	now := time.Now().UTC()
	err = output.Write(context.Background(), observability.Record{
		SchemaVersion: observability.SchemaVersion, Plugin: "builtin.capture", Topic: "capture.request.started",
		RecordID: "trace:00000001", TraceID: "trace", InstanceID: "instance", Sequence: 1,
		OccurredAt: now.Format(time.RFC3339Nano), Data: map[string]any{"method": "POST"}, Time: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-received:
		if message[0] != "dproxy.capture.request.started" {
			t.Fatalf("unexpected tag: %#v", message[0])
		}
	case err = <-errors:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fluent record")
	}
	if status := output.Health(); status.Status != "ok" {
		t.Fatalf("unexpected output health: %#v", status)
	}
}

type unexpectedForwardMessage struct{ value any }

func (e *unexpectedForwardMessage) Error() string { return "unexpected forward message" }
