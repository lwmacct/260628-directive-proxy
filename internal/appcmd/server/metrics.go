package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	vmmetrics "github.com/VictoriaMetrics/metrics"
)

const metricsContentType = "text/plain; version=0.0.4; charset=utf-8"

type runtimeMetrics struct {
	set    *vmmetrics.Set
	prefix string

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

func newRuntimeMetrics(prefix string) *runtimeMetrics {
	set := vmmetrics.NewSet()
	result := &runtimeMetrics{
		set:    set,
		prefix: prefix,
	}
	result.inFlight = set.NewGauge(result.name("in_flight_requests"), nil)
	result.requestSuccessTotal = set.NewCounter(result.name(`requests_total{outcome="success"}`))
	result.requestErrorTotal = set.NewCounter(result.name(`requests_total{outcome="error"}`))
	result.requestCanceledTotal = set.NewCounter(result.name(`requests_total{outcome="canceled"}`))
	result.requestDuration = set.NewPrometheusHistogramExt(result.name("request_duration_seconds"), []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30})
	result.requestBodyBytesTotal = set.NewCounter(result.name("request_body_bytes_total"))
	result.responseBodyBytesTotal = set.NewCounter(result.name("response_body_bytes_total"))
	result.responses1xxTotal = set.NewCounter(result.name(`responses_total{status_class="1xx"}`))
	result.responses2xxTotal = set.NewCounter(result.name(`responses_total{status_class="2xx"}`))
	result.responses3xxTotal = set.NewCounter(result.name(`responses_total{status_class="3xx"}`))
	result.responses4xxTotal = set.NewCounter(result.name(`responses_total{status_class="4xx"}`))
	result.responses5xxTotal = set.NewCounter(result.name(`responses_total{status_class="5xx"}`))
	result.roundTripsTotal = set.NewCounter(result.name("round_trips_total"))
	result.roundTripDuration = set.NewPrometheusHistogramExt(result.name("round_trip_duration_seconds"), []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30})
	result.retriesTotal = set.NewCounter(result.name("retries_total"))
	result.recoveryAttemptsTotal = set.NewCounter(result.name("recovery_attempts_total"))
	result.recoveryFailuresTotal = set.NewCounter(result.name("recovery_failures_total"))
	return result
}

func (m *runtimeMetrics) name(suffix string) string {
	return m.prefix + suffix
}

func (m *runtimeMetrics) Prefix() string {
	if m == nil {
		return ""
	}
	return m.prefix
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
	m.set.NewGauge(m.name("event_output_enabled"), func() float64 { return 0 })
	m.set.NewGauge(m.name("event_output_healthy"), func() float64 { return 0 })
	m.set.NewGauge(m.name("event_output_queue_limit_records"), func() float64 { return 0 })
	m.set.NewGauge(m.name("event_output_queue_limit_bytes"), func() float64 { return 0 })
	m.set.NewGauge(m.name("event_output_queue_records"), func() float64 { return 0 })
	m.set.NewGauge(m.name("event_output_queue_bytes"), func() float64 { return 0 })
	m.set.NewCounter(m.name("event_output_dropped_records_total"))
	m.set.NewCounter(m.name("event_output_failures_total"))
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
	includeRuntime, includeProcess, err := metricsOutputOptions(request)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if handler != nil && handler.set != nil {
		handler.set.WritePrometheus(writer)
	}
	if includeRuntime {
		vmmetrics.WriteGoMetrics(writer)
	}
	if includeProcess {
		vmmetrics.WriteProcMetrics(writer)
	}
	vmmetrics.WritePushMetrics(writer)
}

func metricsOutputOptions(request *http.Request) (includeRuntime, includeProcess bool, err error) {
	if request == nil || request.URL == nil {
		return true, true, nil
	}
	includeRuntime, err = metricsOption(request, "runtime", true)
	if err != nil {
		return false, false, err
	}
	includeProcess, err = metricsOption(request, "process", true)
	if err != nil {
		return false, false, err
	}
	return includeRuntime, includeProcess, nil
}

func metricsOption(request *http.Request, name string, fallback bool) (bool, error) {
	values, ok := request.URL.Query()[name]
	if !ok {
		return fallback, nil
	}
	if len(values) != 1 {
		return false, fmt.Errorf("metrics query parameter %q must be specified once", name)
	}
	switch strings.ToLower(strings.TrimSpace(values[0])) {
	case "1", "true", "on", "yes":
		return true, nil
	case "0", "false", "off", "no":
		return false, nil
	default:
		return false, fmt.Errorf("metrics query parameter %q must be boolean", name)
	}
}
