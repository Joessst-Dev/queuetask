package publisher

// Publisher publishes a message payload to a named topic.
type Publisher interface {
	Publish(topic string, payload []byte) error
}

// Noop is a no-op publisher used when queue-ti integration is disabled.
type Noop struct{}

func (Noop) Publish(_ string, _ []byte) error { return nil }
