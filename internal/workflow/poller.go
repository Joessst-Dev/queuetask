package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	queueti "github.com/Joessst-Dev/queue-ti/clients/go-client"
	"github.com/google/uuid"
)

type watchKey struct {
	instanceID uuid.UUID
	stepName   string
}

// Poller manages gRPC consumer goroutines for steps with trigger=queueti.
// Each goroutine uses the queue-ti streaming Subscribe RPC so messages are
// pushed rather than polled.
type Poller struct {
	mu     sync.Mutex
	active map[watchKey]context.CancelFunc
	engine *Engine
	client *queueti.Client
}

func NewPoller(client *queueti.Client) *Poller {
	return &Poller{
		active: make(map[watchKey]context.CancelFunc),
		client: client,
	}
}

// SetEngine wires the engine reference after construction to avoid circular init.
func (p *Poller) SetEngine(e *Engine) {
	p.engine = e
}

// Watch starts a gRPC consumer goroutine for the given waiting_queueti step.
// It is a no-op if a goroutine is already running for this step.
func (p *Poller) Watch(instanceID uuid.UUID, step *StepExecution) {
	key := watchKey{instanceID, step.StepName}

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.active[key]; exists {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.active[key] = cancel

	group := step.QueueTiGroup
	if group == "" {
		group = fmt.Sprintf("queuetask-%s-%s", instanceID, step.StepName)
	}

	consumer := p.client.NewConsumer(
		step.QueueTiTopic,
		queueti.WithConsumerGroup(group),
	)

	go func() {
		defer func() {
			p.mu.Lock()
			delete(p.active, key)
			p.mu.Unlock()
		}()

		err := consumer.Consume(ctx, func(msgCtx context.Context, msg *queueti.Message) error {
			payload, err := marshalQueueTiPayload(msg)
			if err != nil {
				return fmt.Errorf("marshalling message payload: %w", err)
			}
			if err := p.engine.TriggerStep(msgCtx, instanceID, step.StepName, payload); err != nil {
				slog.Warn("poller trigger failed", "instance_id", instanceID, "step", step.StepName, "error", err)
				return err // Nack so the message is retried
			}
			return nil // Ack
		})

		if err != nil && ctx.Err() == nil {
			slog.Warn("consumer exited with error", "instance_id", instanceID, "step", step.StepName, "error", err)
		}
	}()
}

// marshalQueueTiPayload wraps a queue-ti message into the JSON envelope
// passed as input to engine.TriggerStep and engine.StartInstance.
func marshalQueueTiPayload(msg *queueti.Message) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"message_id": msg.ID,
		"payload":    json.RawMessage(msg.Payload),
		"metadata":   msg.Metadata,
	})
}

// Stop cancels the consumer goroutine for a given step.
func (p *Poller) Stop(instanceID uuid.UUID, stepName string) {
	key := watchKey{instanceID, stepName}
	p.mu.Lock()
	if cancel, ok := p.active[key]; ok {
		cancel()
	}
	p.mu.Unlock()
}
