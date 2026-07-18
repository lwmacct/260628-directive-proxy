package server

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/lwmacct/260714-go-pkg-fluent/pkg/fluent"
	"github.com/tinylib/msgp/msgp"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
)

func TestFluentSinkWritesDPEventOnWire(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	type received struct {
		tag           string
		schemaVersion string
		metadata      map[string]string
		err           error
	}
	receivedRecord := make(chan received, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			receivedRecord <- received{err: acceptErr}
			return
		}
		defer func() { _ = connection.Close() }()
		reader := msgp.NewReader(connection)
		tupleSize, readErr := reader.ReadArrayHeader()
		if readErr != nil || tupleSize != 3 {
			receivedRecord <- received{err: fmt.Errorf("read Forward tuple: size=%d err=%w", tupleSize, readErr)}
			return
		}
		tag, readErr := reader.ReadString()
		if readErr != nil {
			receivedRecord <- received{err: readErr}
			return
		}
		if readErr = reader.Skip(); readErr != nil {
			receivedRecord <- received{err: readErr}
			return
		}
		schemaVersion, fields, readErr := readWireRecord(reader)
		receivedRecord <- received{tag: tag, schemaVersion: schemaVersion, metadata: fields, err: readErr}
	}()

	config := fluent.DefaultConfig()
	config.Endpoint = "tcp://" + listener.Addr().String()
	config.TagPrefix = "dp"
	config.Buffer.BatchWait = 0
	sink := newFluentSink(config)
	if err := sink.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	record := event.Record{
		SchemaVersion: event.SchemaVersion, Producer: "usage", Topic: "llm.usage.observed",
		RecordID: "trace-1:00000001", TraceID: "trace-1", Sequence: 1,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), Time: time.Now().UTC(),
		Metadata: map[string]string{"user_key": "uk_user_1", "trace_id": "trace-1", "tenant_id": "tenant-a"},
		Data:     map[string]any{"total_tokens": int64(13)},
	}
	if err := sink.Write(t.Context(), 0, record); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	result := <-receivedRecord
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.tag != "dp."+record.Topic || result.schemaVersion != event.SchemaVersion || result.metadata["user_key"] != "uk_user_1" || result.metadata["trace_id"] != "trace-1" || result.metadata["tenant_id"] != "tenant-a" {
		t.Fatalf("unexpected Fluent record: tag=%q schema_version=%q metadata=%#v", result.tag, result.schemaVersion, result.metadata)
	}
}

func readWireRecord(reader *msgp.Reader) (string, map[string]string, error) {
	recordSize, err := reader.ReadMapHeader()
	if err != nil {
		return "", nil, err
	}
	var schemaVersion string
	var metadata map[string]string
	for range recordSize {
		key, readErr := reader.ReadString()
		if readErr != nil {
			return "", nil, readErr
		}
		if key == "schema_version" {
			schemaVersion, readErr = reader.ReadString()
			if readErr != nil {
				return "", nil, readErr
			}
			continue
		}
		if key != "metadata" {
			if readErr := reader.Skip(); readErr != nil {
				return "", nil, readErr
			}
			continue
		}
		metadataSize, readErr := reader.ReadMapHeader()
		if readErr != nil {
			return "", nil, readErr
		}
		metadata = make(map[string]string, metadataSize)
		for range metadataSize {
			name, readErr := reader.ReadString()
			if readErr != nil {
				return "", nil, readErr
			}
			value, readErr := reader.ReadString()
			if readErr != nil {
				return "", nil, readErr
			}
			metadata[name] = value
		}
	}
	if schemaVersion == "" || metadata == nil {
		return "", nil, fmt.Errorf("schema_version or metadata field is missing")
	}
	return schemaVersion, metadata, nil
}
