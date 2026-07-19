package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestMetricsOutcomeTreatsProtocolUpgradeAsSuccess(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://proxy.local/socket", nil)
	if outcome := requestMetricsOutcome(request, http.StatusSwitchingProtocols); outcome != "success" {
		t.Fatalf("unexpected protocol upgrade outcome: %q", outcome)
	}
}
