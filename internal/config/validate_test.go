package config

import "testing"

func TestDefaultConfigUsesDedicatedProxyListen(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.HTTP.Listen != ":23198" {
		t.Fatalf("unexpected control listen: %q", cfg.Server.HTTP.Listen)
	}
	if cfg.Proxy.Listen != ":23197" {
		t.Fatalf("unexpected proxy listen: %q", cfg.Proxy.Listen)
	}
}

func TestValidateRejectsMissingProxyListen(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Proxy.Listen = " "

	if _, err := Validate(cfg); err != ErrInvalidProxy {
		t.Fatalf("expected invalid proxy config, got %v", err)
	}
}

func TestValidateRejectsEnabledCaptureWithoutCapacity(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Proxy.Capture.Enabled = true
	cfg.Proxy.Capture.Capacity = 0

	if _, err := Validate(cfg); err != ErrInvalidProxy {
		t.Fatalf("expected invalid proxy config, got %v", err)
	}
}
