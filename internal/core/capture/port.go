package capture

import "time"

type Sink interface {
	Emit(string, Event) error
	Close() error
	CaptureHealth() HealthStatus
}

type HealthStatus struct {
	Status        string
	LastFailureAt time.Time
}

type HealthProvider interface {
	CaptureHealth() HealthStatus
}

type DiscardSink struct{}

func (DiscardSink) Emit(string, Event) error { return nil }
func (DiscardSink) Close() error             { return nil }
func (DiscardSink) CaptureHealth() HealthStatus {
	return HealthStatus{Status: "disabled"}
}
