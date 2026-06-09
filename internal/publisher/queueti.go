package publisher

import (
	"context"
	"fmt"
	"log"

	queueti "github.com/Joessst-Dev/queue-ti/clients/go-client"
)

// QueueTiProducer publishes messages via the queue-ti gRPC client.
type QueueTiProducer struct {
	client   *queueti.Client
	producer *queueti.Producer
}

// NewQueueTiProducer dials the queue-ti gRPC server and optionally
// authenticates using the admin HTTP API.
func NewQueueTiProducer(ctx context.Context, grpcAddr, adminURL, username, password string) (*QueueTiProducer, error) {
	opts := []queueti.DialOption{queueti.WithInsecure()}

	if username != "" || password != "" {
		auth, err := queueti.NewAuth(ctx, adminURL, username, password)
		if err != nil {
			return nil, fmt.Errorf("queue-ti auth: %w", err)
		}
		if token := auth.Token(); token != "" {
			opts = append(opts,
				queueti.WithBearerToken(token),
				queueti.WithTokenRefresher(auth.Refresh),
			)
		}
	}

	client, err := queueti.Dial(grpcAddr, opts...)
	if err != nil {
		return nil, fmt.Errorf("dialing queue-ti at %s: %w", grpcAddr, err)
	}

	return &QueueTiProducer{
		client:   client,
		producer: client.NewProducer(),
	}, nil
}

func (p *QueueTiProducer) Publish(topic string, payload []byte) error {
	_, err := p.producer.Publish(context.Background(), topic, payload)
	if err != nil {
		return fmt.Errorf("publishing to topic %q: %w", topic, err)
	}
	return nil
}

func (p *QueueTiProducer) Close() {
	if err := p.client.Close(); err != nil {
		log.Printf("warn: closing queue-ti producer client: %v", err)
	}
}
