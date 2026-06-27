package kafka

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidConfig = errors.New("invalid kafka config")

const (
	DefaultTopicPrefix = "prod.directive-proxy"
)

type Config struct {
	Brokers                []string
	TopicPrefix            string
	SASL                   SASL
	EnsureTopics           bool
	TopicPartitions        int32
	TopicReplicationFactor int16
	PublishTimeout         time.Duration
	MaxPublishRetries      int
}

type SASL struct {
	Username string
	Password string
}

func (c Config) Validate() error {
	if len(c.Brokers) == 0 {
		return ErrInvalidConfig
	}
	if strings.TrimSpace(c.TopicPrefix) == "" {
		return ErrInvalidConfig
	}
	if (c.SASL.Username == "") != (c.SASL.Password == "") {
		return ErrInvalidConfig
	}
	return nil
}

func (c Config) withDefaults() Config {
	return c.withDefaultsAt(time.Now())
}

func (c Config) withDefaultsAt(now time.Time) Config {
	if strings.TrimSpace(c.TopicPrefix) == "" {
		c.TopicPrefix = DefaultTopicPrefix
	}
	if c.TopicPartitions <= 0 {
		c.TopicPartitions = 1
	}
	if c.TopicReplicationFactor <= 0 {
		c.TopicReplicationFactor = 1
	}
	if c.PublishTimeout <= 0 {
		c.PublishTimeout = 10 * time.Second
	}
	if c.MaxPublishRetries <= 0 {
		c.MaxPublishRetries = 3
	}
	ts := fmt.Sprintf("%d", now.Unix())
	c.TopicPrefix = replaceTopicTimestamp(c.TopicPrefix, ts)
	return c
}

func replaceTopicTimestamp(topic string, ts string) string {
	topic = strings.TrimSpace(topic)
	if topic == "" || ts == "" {
		return topic
	}
	return strings.ReplaceAll(topic, "{ts}", ts)
}
