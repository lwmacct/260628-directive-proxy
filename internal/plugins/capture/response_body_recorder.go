package capture

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
)

type responseBodyRecorder struct {
	inner       io.ReadCloser
	ctx         context.Context
	publisher   eventbus.Publisher
	idGen       eventbus.IDGenerator
	requestID   string
	labels      map[string]any
	runtime     eventbus.Runtime
	contentType string
	startTime   time.Time
	metadata    captureMetadata
	buf         bytes.Buffer
	total       int
	complete    bool
	readErr     error
	publishOnce sync.Once
}

func newResponseBodyRecorder(inner io.ReadCloser, ctx context.Context, publisher eventbus.Publisher, idGen eventbus.IDGenerator, requestID string, labels map[string]any, runtime eventbus.Runtime, contentType string, start time.Time, metadata captureMetadata) *responseBodyRecorder {
	if publisher == nil {
		publisher = eventbus.NopPublisher{}
	}
	if idGen == nil {
		idGen = eventbus.NewIDGenerator()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &responseBodyRecorder{
		inner:       inner,
		ctx:         ctx,
		publisher:   publisher,
		idGen:       idGen,
		requestID:   requestID,
		labels:      cloneLabelsMap(labels),
		runtime:     eventbus.CloneRuntime(runtime),
		contentType: contentType,
		startTime:   start,
		metadata:    metadata,
	}
}

func (r *responseBodyRecorder) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	if n > 0 {
		r.buf.Write(p[:n])
		r.total += n
	}
	switch {
	case err == io.EOF:
		r.complete = true
		r.publish()
	case err != nil:
		r.readErr = err
		r.publish()
	}
	return n, err
}

func (r *responseBodyRecorder) Close() error {
	err := r.inner.Close()
	if err != nil && r.readErr == nil {
		r.readErr = err
	}
	r.publish()
	return err
}

func (r *responseBodyRecorder) publish() {
	r.publishOnce.Do(func() {
		data := ResponseBodyData{
			ContentType: r.contentType,
			Size:        r.total,
			Duration:    time.Since(r.startTime),
			Complete:    r.complete,
		}
		applyResponseBodyMetadata(&data, r.metadata)
		if r.buf.Len() > 0 {
			data.Body = buildBody(r.buf.Bytes(), r.contentType)
		}
		if r.readErr != nil && r.readErr != io.EOF {
			data.Error = r.readErr.Error()
		}
		_ = r.publisher.Publish(r.ctx, eventbus.Event{
			EventID:   r.idGen.Generate(),
			RequestID: r.requestID,
			Type:      EventTypeResponseBody,
			Timestamp: time.Now(),
			Labels:    cloneLabelsMap(r.labels),
			Runtime:   eventbus.CloneRuntime(r.runtime),
			Data:      data,
		})
	})
}
