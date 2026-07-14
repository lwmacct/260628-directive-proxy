package sse

import (
	"bytes"
	"strconv"
	"strings"
)

type Event struct {
	Sequence    uint64
	Type        string
	Data        string
	ID          string
	RetryMillis *int64
	Truncated   bool
}

type Parser struct {
	maxEventBytes int
	pending       []byte
	data          []string
	eventType     string
	lastEventID   string
	retryMillis   *int64
	eventBytes    int
	truncated     bool
	sequence      uint64
	onEvent       func(Event)
	onComment     func(string)
	firstLine     bool
}

func NewParser(maxEventBytes int, onEvent func(Event), onComment func(string)) *Parser {
	if maxEventBytes <= 0 {
		maxEventBytes = 1 << 20
	}
	return &Parser{maxEventBytes: maxEventBytes, onEvent: onEvent, onComment: onComment, firstLine: true}
}

func (p *Parser) Feed(data []byte) {
	if p == nil || len(data) == 0 {
		return
	}
	p.pending = append(p.pending, data...)
	for {
		index := bytes.IndexAny(p.pending, "\r\n")
		if index < 0 {
			limit := p.maxEventBytes + 64
			if len(p.pending) > limit {
				if bytes.HasPrefix(p.pending, []byte("data")) || len(p.data) > 0 {
					p.truncated = true
				}
				p.pending = p.pending[:limit]
			}
			return
		}
		if p.pending[index] == '\r' && index+1 == len(p.pending) {
			return
		}
		line := string(p.pending[:index])
		consume := index + 1
		if p.pending[index] == '\r' && consume < len(p.pending) && p.pending[consume] == '\n' {
			consume++
		}
		p.pending = p.pending[consume:]
		p.processLine(line)
	}
}

func (p *Parser) Close() {
	if p == nil {
		return
	}
	if len(p.pending) > 0 && p.pending[len(p.pending)-1] == '\r' {
		p.Feed([]byte{'\n'})
	}
	p.pending = nil
	p.resetEvent()
}

func (p *Parser) processLine(line string) {
	if p.firstLine {
		line = strings.TrimPrefix(line, "\ufeff")
		p.firstLine = false
	}
	if line == "" {
		p.dispatch()
		return
	}
	if strings.HasPrefix(line, ":") {
		if p.onComment != nil {
			p.onComment(strings.TrimPrefix(line, ":"))
		}
		return
	}
	field, value, found := strings.Cut(line, ":")
	if !found {
		value = ""
	} else {
		value = strings.TrimPrefix(value, " ")
	}
	switch field {
	case "data":
		p.appendData(value)
	case "event":
		p.eventType = value
	case "id":
		if !strings.ContainsRune(value, '\x00') {
			p.lastEventID = value
		}
	case "retry":
		if value == "" {
			return
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err == nil && parsed >= 0 {
			p.retryMillis = &parsed
		}
	}
}

func (p *Parser) appendData(value string) {
	p.eventBytes += len(value)
	if p.eventBytes > p.maxEventBytes {
		p.truncated = true
		return
	}
	p.data = append(p.data, value)
}

func (p *Parser) dispatch() {
	if len(p.data) == 0 && !p.truncated {
		p.resetEvent()
		return
	}
	p.sequence++
	eventType := p.eventType
	if eventType == "" {
		eventType = "message"
	}
	if p.onEvent != nil {
		p.onEvent(Event{
			Sequence:    p.sequence,
			Type:        eventType,
			Data:        strings.Join(p.data, "\n"),
			ID:          p.lastEventID,
			RetryMillis: p.retryMillis,
			Truncated:   p.truncated,
		})
	}
	p.resetEvent()
}

func (p *Parser) resetEvent() {
	p.data = nil
	p.eventType = ""
	p.retryMillis = nil
	p.eventBytes = 0
	p.truncated = false
}
