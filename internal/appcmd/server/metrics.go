package server

import (
	"net/http"
	"time"

	vmmetrics "github.com/VictoriaMetrics/metrics"
)

const metricsContentType = "text/plain; version=0.0.4; charset=utf-8"

type runtimeMetrics struct {
	set *vmmetrics.Set

	inFlight               *vmmetrics.Gauge
	requestSuccessTotal    *vmmetrics.Counter
	requestErrorTotal      *vmmetrics.Counter
	requestCanceledTotal   *vmmetrics.Counter
	requestDuration        *vmmetrics.PrometheusHistogram
	requestBodyBytesTotal  *vmmetrics.Counter
	responseBodyBytesTotal *vmmetrics.Counter
	responses1xxTotal      *vmmetrics.Counter
	responses2xxTotal      *vmmetrics.Counter
	responses3xxTotal      *vmmetrics.Counter
	responses4xxTotal      *vmmetrics.Counter
	responses5xxTotal      *vmmetrics.Counter

	roundTripsTotal       *vmmetrics.Counter
	roundTripDuration     *vmmetrics.PrometheusHistogram
	retriesTotal          *vmmetrics.Counter
	recoveryAttemptsTotal *vmmetrics.Counter
	recoveryFailuresTotal *vmmetrics.Counter
}

func newRuntimeMetrics() *runtimeMetrics {
	set := vmmetrics.NewSet()
	return &runtimeMetrics{
		set:                    set,
		inFlight:               set.NewGauge("directive_proxy_in_flight_requests", nil),
		requestSuccessTotal:    set.NewCounter(`directive_proxy_requests_total{outcome="success"}`),
		requestErrorTotal:      set.NewCounter(`directive_proxy_requests_total{outcome="error"}`),
		requestCanceledTotal:   set.NewCounter(`directive_proxy_requests_total{outcome="canceled"}`),
		requestDuration:        set.NewPrometheusHistogramExt("directive_proxy_request_duration_seconds", []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30}),
		requestBodyBytesTotal:  set.NewCounter("directive_proxy_request_body_bytes_total"),
		responseBodyBytesTotal: set.NewCounter("directive_proxy_response_body_bytes_total"),
		responses1xxTotal:      set.NewCounter(`directive_proxy_responses_total{status_class="1xx"}`),
		responses2xxTotal:      set.NewCounter(`directive_proxy_responses_total{status_class="2xx"}`),
		responses3xxTotal:      set.NewCounter(`directive_proxy_responses_total{status_class="3xx"}`),
		responses4xxTotal:      set.NewCounter(`directive_proxy_responses_total{status_class="4xx"}`),
		responses5xxTotal:      set.NewCounter(`directive_proxy_responses_total{status_class="5xx"}`),
		roundTripsTotal:        set.NewCounter("directive_proxy_round_trips_total"),
		roundTripDuration:      set.NewPrometheusHistogramExt("directive_proxy_round_trip_duration_seconds", []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30}),
		retriesTotal:           set.NewCounter("directive_proxy_retries_total"),
		recoveryAttemptsTotal:  set.NewCounter("directive_proxy_recovery_attempts_total"),
		recoveryFailuresTotal:  set.NewCounter("directive_proxy_recovery_failures_total"),
	}
}

func (m *runtimeMetrics) MetricsSet() *vmmetrics.Set {
	if m == nil {
		return nil
	}
	return m.set
}

func (m *runtimeMetrics) RegisterDisabledEventOutput() {
	if m == nil || m.set == nil {
		return
	}
	m.set.NewGauge("directive_proxy_event_output_enabled", func() float64 { return 0 })
	m.set.NewGauge("directive_proxy_event_output_healthy", func() float64 { return 0 })
	m.set.NewGauge("directive_proxy_event_output_queue_limit_records", func() float64 { return 0 })
	m.set.NewGauge("directive_proxy_event_output_queue_limit_bytes", func() float64 { return 0 })
	m.set.NewGauge("directive_proxy_event_output_queue_records", func() float64 { return 0 })
	m.set.NewGauge("directive_proxy_event_output_queue_bytes", func() float64 { return 0 })
	m.set.NewCounter("directive_proxy_event_output_dropped_records_total")
	m.set.NewCounter("directive_proxy_event_output_failures_total")
}

func (m *runtimeMetrics) RequestStarted() {
	if m != nil {
		m.inFlight.Inc()
	}
}

func (m *runtimeMetrics) RequestFinished(status int, outcome string, duration time.Duration, requestBodyBytes, responseBodyBytes int64) {
	if m == nil {
		return
	}
	m.inFlight.Dec()
	switch outcome {
	case "success":
		m.requestSuccessTotal.Inc()
	case "canceled":
		m.requestCanceledTotal.Inc()
	default:
		m.requestErrorTotal.Inc()
	}
	if requestBodyBytes > 0 {
		m.requestBodyBytesTotal.AddInt64(requestBodyBytes)
	}
	if responseBodyBytes > 0 {
		m.responseBodyBytesTotal.AddInt64(responseBodyBytes)
	}
	m.requestDuration.Update(duration.Seconds())
	switch {
	case status >= 100 && status < 200:
		m.responses1xxTotal.Inc()
	case status >= 200 && status < 300:
		m.responses2xxTotal.Inc()
	case status >= 300 && status < 400:
		m.responses3xxTotal.Inc()
	case status >= 400 && status < 500:
		m.responses4xxTotal.Inc()
	case status >= 500:
		m.responses5xxTotal.Inc()
	}
}

func (m *runtimeMetrics) RoundTripStarted() {
	if m != nil {
		m.roundTripsTotal.Inc()
	}
}

func (m *runtimeMetrics) RoundTripFinished(outcome string, duration time.Duration) {
	if m == nil {
		return
	}
	m.roundTripDuration.Update(duration.Seconds())
	if outcome == "canceled_for_retry" {
		m.retriesTotal.Inc()
	}
}

func (m *runtimeMetrics) RecoveryStarted(string) {
	if m != nil {
		m.recoveryAttemptsTotal.Inc()
	}
}

func (m *runtimeMetrics) RecoveryFinished(outcome string) {
	if m != nil && outcome != "forwarded" && outcome != "retry_requested" {
		m.recoveryFailuresTotal.Inc()
	}
}

type metricsHandler struct {
	set *vmmetrics.Set
}

func (handler *metricsHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request == nil || request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writer.Header().Set("Content-Type", metricsContentType)
	writer.Header().Set("Cache-Control", "no-store")
	if handler != nil && handler.set != nil {
		handler.set.WritePrometheus(writer)
	}
	vmmetrics.WriteProcessMetrics(writer)
}
