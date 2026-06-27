package jsoncapture

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Mode string

const (
	ModeInclude Mode = "include"
	ModeExclude Mode = "exclude"
)

type Options struct {
	ObjectPath []string
	Mode       Mode
	Fields     []string
}

type Result struct {
	Fields map[string]json.RawMessage
	Found  bool
}

func Capture(r io.Reader, opts Options) (Result, error) {
	scanner := NewScanner(opts)
	var buf [8192]byte
	for {
		n, err := r.Read(buf[:])
		if n > 0 {
			if writeErr := scanner.Write(buf[:n]); writeErr != nil {
				return Result{}, writeErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return Result{}, err
		}
	}
	return scanner.Finish()
}

type fieldPolicy struct {
	mode   Mode
	fields map[string]struct{}
}

func newFieldPolicy(mode Mode, fields []string) fieldPolicy {
	switch Mode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case ModeExclude:
		mode = ModeExclude
	default:
		mode = ModeInclude
	}
	out := fieldPolicy{
		mode:   mode,
		fields: make(map[string]struct{}, len(fields)),
	}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			out.fields[field] = struct{}{}
		}
	}
	return out
}

func (p fieldPolicy) capture(field string) bool {
	_, ok := p.fields[field]
	if p.mode == ModeExclude {
		return !ok
	}
	return ok
}

const (
	jsonObject byte = '{'
	jsonArray  byte = '['

	objExpectKey = iota
	objExpectColon
	objExpectValue
	objAfterValue
	arrayExpectValue
	arrayAfterValue
)

type jsonContext struct {
	kind       byte
	state      int
	pendingKey string
	pathMatch  int
}

type Scanner struct {
	stack []jsonContext

	inString      bool
	escape        bool
	captureString bool
	stringBuf     []byte

	inPrimitive bool

	objectPath []string
	policy     fieldPolicy
	fields     map[string]json.RawMessage
	found      bool
	err        error

	capturingValue bool
	valueKey       string
	valueDepth     int
	valueInString  bool
	valueEscape    bool
	valuePrimitive bool
	valueBuf       []byte
}

func NewScanner(opts Options) *Scanner {
	path := make([]string, 0, len(opts.ObjectPath))
	for _, part := range opts.ObjectPath {
		part = strings.TrimSpace(part)
		if part != "" {
			path = append(path, part)
		}
	}
	return &Scanner{
		objectPath: path,
		policy:     newFieldPolicy(opts.Mode, opts.Fields),
	}
}

func (s *Scanner) Write(data []byte) error {
	for _, b := range data {
		if err := s.writeByte(b); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scanner) Finish() (Result, error) {
	if s.err != nil {
		return Result{}, s.err
	}
	if s.capturingValue {
		if s.valuePrimitive {
			if err := s.finishValueCapture(); err != nil {
				return Result{}, err
			}
		} else {
			return Result{}, fmt.Errorf("incomplete captured JSON value")
		}
	}
	return Result{Fields: s.fields, Found: s.found}, nil
}

func (s *Scanner) writeByte(b byte) error {
	if s.err != nil {
		return s.err
	}
	if s.capturingValue {
		reprocess, err := s.writeValueByte(b)
		if err != nil || !reprocess {
			return err
		}
	}
	if s.inString {
		s.writeStringByte(b)
		return nil
	}
	if s.inPrimitive {
		if isJSONDelimiter(b) {
			s.inPrimitive = false
			s.completeValue()
			if isJSONWhitespace(b) {
				return nil
			}
		} else {
			return nil
		}
	}
	if isJSONWhitespace(b) {
		return nil
	}

	switch b {
	case '"':
		if s.shouldCaptureValue() {
			s.startValueCapture('"')
		} else {
			s.startString()
		}
	case '{':
		if s.shouldCaptureValue() {
			s.startValueCapture('{')
		} else {
			s.startObject()
		}
	case '[':
		if s.shouldCaptureValue() {
			s.startValueCapture('[')
		} else {
			s.startArray()
		}
	case ':':
		if ctx := s.top(); ctx != nil && ctx.kind == jsonObject && ctx.state == objExpectColon {
			ctx.state = objExpectValue
		}
	case ',':
		s.nextValue()
	case '}':
		s.endContainer(jsonObject)
	case ']':
		s.endContainer(jsonArray)
	default:
		if s.shouldCaptureValue() {
			s.startValueCapture(b)
		} else {
			s.startPrimitive()
		}
	}
	return s.err
}

func (s *Scanner) startString() {
	s.inString = true
	s.escape = false
	s.captureString = s.shouldCaptureString()
	if s.captureString {
		s.stringBuf = append(s.stringBuf[:0], '"')
	}
}

func (s *Scanner) writeStringByte(b byte) {
	if s.captureString {
		s.stringBuf = append(s.stringBuf, b)
	}
	if s.escape {
		s.escape = false
		return
	}
	if b == '\\' {
		s.escape = true
		return
	}
	if b != '"' {
		return
	}

	raw := s.stringBuf
	captureString := s.captureString
	s.inString = false
	s.captureString = false

	ctx := s.top()
	if ctx == nil {
		s.completeValue()
		return
	}
	if ctx.kind == jsonObject && ctx.state == objExpectKey {
		if !captureString {
			ctx.pendingKey = ""
			ctx.state = objExpectColon
			return
		}
		decoded, err := strconv.Unquote(string(raw))
		if err != nil {
			s.err = err
			return
		}
		ctx.pendingKey = decoded
		ctx.state = objExpectColon
		return
	}
	s.completeValue()
}

func (s *Scanner) shouldCaptureString() bool {
	ctx := s.top()
	if ctx == nil || ctx.kind != jsonObject || ctx.state != objExpectKey {
		return false
	}
	return s.needsObjectKey(ctx)
}

func (s *Scanner) needsObjectKey(ctx *jsonContext) bool {
	if s.inTargetObject(ctx) {
		return true
	}
	if ctx.pathMatch >= len(s.objectPath) {
		return false
	}
	return ctx.pathMatch > 0 || len(s.stack) == 1
}

func (s *Scanner) shouldCaptureValue() bool {
	ctx := s.top()
	if ctx == nil || ctx.kind != jsonObject || ctx.state != objExpectValue || !s.inTargetObject(ctx) {
		return false
	}
	return s.policy.capture(ctx.pendingKey)
}

func (s *Scanner) startObject() {
	match := 0
	parent := s.top()
	if parent != nil && parent.kind == jsonObject && parent.state == objExpectValue {
		if parent.pathMatch < len(s.objectPath) &&
			(parent.pathMatch > 0 || len(s.stack) == 1) &&
			parent.pendingKey == s.objectPath[parent.pathMatch] {
			match = parent.pathMatch + 1
		}
	}
	if parent == nil && len(s.objectPath) == 0 {
		match = 0
	}
	s.stack = append(s.stack, jsonContext{kind: jsonObject, state: objExpectKey, pathMatch: match})
	if s.inTargetObject(&s.stack[len(s.stack)-1]) {
		s.found = true
	}
}

func (s *Scanner) startArray() {
	s.stack = append(s.stack, jsonContext{kind: jsonArray, state: arrayExpectValue})
}

func (s *Scanner) startPrimitive() {
	s.inPrimitive = true
}

func (s *Scanner) endContainer(kind byte) {
	if len(s.stack) == 0 {
		return
	}
	ctx := s.stack[len(s.stack)-1]
	if ctx.kind != kind {
		s.err = fmt.Errorf("unexpected JSON container close")
		return
	}
	s.stack = s.stack[:len(s.stack)-1]
	s.completeValue()
}

func (s *Scanner) nextValue() {
	ctx := s.top()
	if ctx == nil {
		return
	}
	switch {
	case ctx.kind == jsonObject && ctx.state == objAfterValue:
		ctx.pendingKey = ""
		ctx.state = objExpectKey
	case ctx.kind == jsonArray && ctx.state == arrayAfterValue:
		ctx.state = arrayExpectValue
	}
}

func (s *Scanner) completeValue() {
	ctx := s.top()
	if ctx == nil {
		return
	}
	switch ctx.kind {
	case jsonObject:
		if ctx.state == objExpectValue {
			ctx.pendingKey = ""
			ctx.state = objAfterValue
		}
	case jsonArray:
		if ctx.state == arrayExpectValue {
			ctx.state = arrayAfterValue
		}
	}
}

func (s *Scanner) startValueCapture(first byte) {
	ctx := s.top()
	s.capturingValue = true
	s.valueKey = ctx.pendingKey
	s.valueDepth = 0
	s.valueInString = false
	s.valueEscape = false
	s.valuePrimitive = false
	s.valueBuf = append(s.valueBuf[:0], first)
	switch first {
	case '"':
		s.valueInString = true
	case '{', '[':
		s.valueDepth = 1
	default:
		s.valuePrimitive = true
	}
}

func (s *Scanner) writeValueByte(b byte) (bool, error) {
	if s.valuePrimitive {
		if isJSONDelimiter(b) {
			if err := s.finishValueCapture(); err != nil {
				return false, err
			}
			return true, nil
		}
		s.valueBuf = append(s.valueBuf, b)
		return false, nil
	}

	s.valueBuf = append(s.valueBuf, b)
	if s.valueInString {
		if s.valueEscape {
			s.valueEscape = false
			return false, nil
		}
		if b == '\\' {
			s.valueEscape = true
			return false, nil
		}
		if b == '"' {
			s.valueInString = false
			if s.valueDepth == 0 {
				return false, s.finishValueCapture()
			}
		}
		return false, nil
	}
	switch b {
	case '"':
		s.valueInString = true
	case '{', '[':
		s.valueDepth++
	case '}', ']':
		s.valueDepth--
		if s.valueDepth == 0 {
			return false, s.finishValueCapture()
		}
	}
	return false, nil
}

func (s *Scanner) finishValueCapture() error {
	raw := append(json.RawMessage(nil), s.valueBuf...)
	if !json.Valid(raw) {
		return fmt.Errorf("invalid captured JSON value")
	}
	if s.fields == nil {
		s.fields = make(map[string]json.RawMessage)
	}
	s.fields[s.valueKey] = raw
	s.capturingValue = false
	s.valueKey = ""
	s.valueBuf = s.valueBuf[:0]
	s.completeValue()
	return nil
}

func (s *Scanner) inTargetObject(ctx *jsonContext) bool {
	if ctx == nil || ctx.kind != jsonObject {
		return false
	}
	if len(s.objectPath) == 0 {
		return len(s.stack) == 1
	}
	return ctx.pathMatch == len(s.objectPath)
}

func (s *Scanner) top() *jsonContext {
	if len(s.stack) == 0 {
		return nil
	}
	return &s.stack[len(s.stack)-1]
}

func isJSONDelimiter(b byte) bool {
	return isJSONWhitespace(b) || b == ',' || b == '}' || b == ']'
}

func isJSONWhitespace(b byte) bool {
	switch b {
	case ' ', '\n', '\r', '\t':
		return true
	default:
		return false
	}
}
