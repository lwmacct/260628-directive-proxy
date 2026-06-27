package proxyplan

import "testing"

func TestCapturePolicyWithDefaultsMakesHeadersEffectiveForBodies(t *testing.T) {
	policy := CapturePolicy{
		Configured:      true,
		RequestHeaders:  false,
		ResponseHeaders: false,
		RequestBody:     true,
		ResponseBody:    true,
	}.WithDefaults()

	if !policy.RequestHeaders {
		t.Fatal("expected request body recording to make request headers effective")
	}
	if !policy.ResponseHeaders {
		t.Fatal("expected response body recording to make response headers effective")
	}
}
