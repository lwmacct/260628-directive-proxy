package fluentcapture

import (
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/tinylib/msgp/msgp"

	"github.com/lwmacct/260628-directive-proxy/internal/core/capture"
)

func TestExporterSendsAcknowledgedForwardRecord(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan []any, 1)
	errors := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			errors <- acceptErr
			return
		}
		defer conn.Close()
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
	exporter, err := New(Config{
		Network:            "tcp",
		Host:               host,
		Port:               port,
		Connections:        1,
		Timeout:            time.Second,
		WriteTimeout:       time.Second,
		ReadTimeout:        time.Second,
		RetryWaitMillis:    1,
		MaxRetry:           1,
		MaxRetryWaitMillis: 1,
		TagPrefix:          "dproxy.capture",
		RequestAck:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer exporter.Close()
	now := time.Now().UTC()
	err = exporter.Emit("lifecycle", capture.Event{
		SchemaVersion: capture.SchemaVersion,
		RecordID:      "trace:0001",
		TraceID:       "trace",
		InstanceID:    "instance",
		Sequence:      1,
		Kind:          "request.started",
		OccurredAt:    now.Format(time.RFC3339Nano),
		Data:          map[string]any{"method": "POST"},
		Time:          now,
	})
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	select {
	case message := <-received:
		if message[0] != "dproxy.capture.lifecycle" {
			t.Fatalf("unexpected tag: %#v", message[0])
		}
	case err = <-errors:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fluent record")
	}
	if status := exporter.CaptureHealth(); status.Status != "ok" {
		t.Fatalf("unexpected capture health: %#v", status)
	}
}

type unexpectedForwardMessage struct{ value any }

func (e *unexpectedForwardMessage) Error() string { return "unexpected forward message" }
