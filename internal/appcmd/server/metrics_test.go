package server

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRuntimeMetricsClassifyProtocolUpgrade(t *testing.T) {
	metrics := newRuntimeMetrics("m_260628_")
	metrics.RequestStarted()
	metrics.RequestFinished(http.StatusSwitchingProtocols, "success", time.Second, 0, 0)

	var output bytes.Buffer
	metrics.MetricsSet().WritePrometheus(&output)
	for _, metric := range []string{
		`m_260628_requests_total{outcome="success"} 1`,
		`m_260628_responses_total{status_class="1xx"} 1`,
	} {
		if !strings.Contains(output.String(), metric) {
			t.Fatalf("metrics output is missing %q: %s", metric, output.String())
		}
	}
}

func TestRuntimeMetricsTrackRetriesAndRecoveryFailures(t *testing.T) {
	metrics := newRuntimeMetrics("m_260628_")
	metrics.RoundTripStarted()
	metrics.RoundTripFinished("canceled_for_retry", time.Second)
	metrics.RecoveryStarted("unexpected_status")
	metrics.RecoveryFinished("retry_requested")
	metrics.RecoveryStarted("transport_error")
	metrics.RecoveryFinished("failed")

	var output bytes.Buffer
	metrics.MetricsSet().WritePrometheus(&output)
	for _, metric := range []string{
		"m_260628_round_trips_total 1",
		"m_260628_retries_total 1",
		"m_260628_recovery_attempts_total 2",
		"m_260628_recovery_failures_total 1",
	} {
		if !strings.Contains(output.String(), metric) {
			t.Fatalf("metrics output is missing %q: %s", metric, output.String())
		}
	}
}
