package sse

import "testing"

func TestParserRecoversEventsAcrossArbitraryChunks(t *testing.T) {
	var events []Event
	var comments []string
	parser := NewParser(1024, func(event Event) { events = append(events, event) }, func(comment string) { comments = append(comments, comment) })
	for _, chunk := range []string{
		"\ufeffid: 7\r", "\nevent: update\r\ndata: first", "\ndata: second\r\nretry: 1500\r\n\r", "\n: heartbeat\n\ndata: next\n\n",
	} {
		parser.Feed([]byte(chunk))
	}
	parser.Close()

	if len(events) != 2 {
		t.Fatalf("unexpected events: %#v", events)
	}
	if events[0].ID != "7" || events[0].Type != "update" || events[0].Data != "first\nsecond" || events[0].RetryMillis == nil || *events[0].RetryMillis != 1500 {
		t.Fatalf("unexpected first event: %#v", events[0])
	}
	if events[1].ID != "7" || events[1].Type != "message" || events[1].Data != "next" {
		t.Fatalf("unexpected second event: %#v", events[1])
	}
	if len(comments) != 1 || comments[0] != " heartbeat" {
		t.Fatalf("unexpected comments: %#v", comments)
	}
}

func TestParserMarksOversizedEventAndHandlesCarriageReturnTerminator(t *testing.T) {
	var events []Event
	parser := NewParser(3, func(event Event) { events = append(events, event) }, nil)
	parser.Feed([]byte("data: long\r\r"))
	parser.Close()

	if len(events) != 1 || !events[0].Truncated || events[0].Data != "" {
		t.Fatalf("unexpected oversized event: %#v", events)
	}
}

func TestParserBoundsUnterminatedDataLine(t *testing.T) {
	parser := NewParser(16, nil, nil)
	for range 1000 {
		parser.Feed([]byte("data: abcdefghijklmnopqrstuvwxyz"))
	}
	if len(parser.pending) > parser.maxEventBytes+64 {
		t.Fatalf("parser retained an unbounded line: %d", len(parser.pending))
	}
}
