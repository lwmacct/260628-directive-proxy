package kafka

import (
	"testing"
	"time"
)

func TestConfigWithDefaultsFillsTopicPrefix(t *testing.T) {
	cfg := Config{}.withDefaults()
	if cfg.TopicPrefix != DefaultTopicPrefix {
		t.Fatalf("unexpected topic prefix: %s", cfg.TopicPrefix)
	}
}

func TestConfigWithDefaultsReplacesTimestampPlaceholder(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	cfg := Config{
		TopicPrefix: "prod.directive-proxy.{ts}",
	}.withDefaultsAt(at)

	if cfg.TopicPrefix != "prod.directive-proxy.1700000000" {
		t.Fatalf("unexpected topic prefix: %s", cfg.TopicPrefix)
	}
}
