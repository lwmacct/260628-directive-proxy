package capture

import "net/http"

type headerCountingWriter struct {
	n       int
	seenEnd bool
	tail    []byte
}

func (w *headerCountingWriter) Write(p []byte) (int, error) {
	if w.seenEnd {
		return len(p), nil
	}
	for _, b := range p {
		if w.seenEnd {
			break
		}
		w.n++
		w.tail = append(w.tail, b)
		if len(w.tail) > 4 {
			w.tail = w.tail[len(w.tail)-4:]
		}
		if len(w.tail) == 4 &&
			w.tail[0] == '\r' &&
			w.tail[1] == '\n' &&
			w.tail[2] == '\r' &&
			w.tail[3] == '\n' {
			w.seenEnd = true
		}
	}
	return len(p), nil
}

func countRequestBytes(req *http.Request, body []byte) int {
	if req == nil {
		return 0
	}
	headerBytes := countRequestHeaderBytes(req, len(body))
	if headerBytes == 0 && len(body) == 0 {
		return 0
	}
	return headerBytes + len(body)
}

func countRequestHeaderBytes(req *http.Request, bodyLen int) int {
	if req == nil {
		return 0
	}
	reqCopy := req.Clone(req.Context())
	reqCopy.Body = http.NoBody
	if len(reqCopy.TransferEncoding) == 0 {
		reqCopy.ContentLength = int64(bodyLen)
	}
	writer := &headerCountingWriter{tail: make([]byte, 0, 4)}
	if err := reqCopy.Write(writer); err != nil {
		return 0
	}
	return writer.n
}

func countResponseBytes(resp *http.Response, body []byte) int {
	if resp == nil {
		return 0
	}
	headerBytes := countResponseHeaderBytes(resp, len(body))
	if headerBytes == 0 && len(body) == 0 {
		return 0
	}
	return headerBytes + len(body)
}

func countResponseHeaderBytes(resp *http.Response, bodyLen int) int {
	if resp == nil {
		return 0
	}
	respCopy := *resp
	respCopy.Body = http.NoBody
	if len(respCopy.TransferEncoding) == 0 && bodyLen >= 0 {
		respCopy.ContentLength = int64(bodyLen)
	}
	writer := &headerCountingWriter{tail: make([]byte, 0, 4)}
	if err := respCopy.Write(writer); err != nil {
		return 0
	}
	return writer.n
}
