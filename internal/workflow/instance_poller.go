package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	queueti "github.com/Joessst-Dev/queue-ti/clients/go-client"
)

// InstancePoller manages gRPC consumer goroutines that start new workflow
// instances when a message arrives on a configured topic.
type InstancePoller struct {
	mu     sync.Mutex
	active map[string]context.CancelFunc // key: "workflowName:topic"
	engine *Engine
	client *queueti.Client
}

func NewInstancePoller(client *queueti.Client, engine *Engine) *InstancePoller {
	return &InstancePoller{
		active: make(map[string]context.CancelFunc),
		engine: engine,
		client: client,
	}
}

// Sync reconciles running goroutines with the current definitions. Called on
// startup and after every registry reload.
func (p *InstancePoller) Sync(defs []*Definition) {
	p.mu.Lock()
	defer p.mu.Unlock()

	desired := make(map[string]WorkflowTrigger)
	for _, def := range defs {
		for _, t := range def.Triggers {
			if t.Type != InstanceTriggerQueueTi {
				continue
			}
			key := def.Name + ":" + t.Topic
			desired[key] = t
			if _, exists := p.active[key]; !exists {
				ctx, cancel := context.WithCancel(context.Background())
				p.active[key] = cancel
				go p.run(ctx, def.Name, t)
			}
		}
	}

	// Cancel goroutines whose trigger no longer exists.
	for key, cancel := range p.active {
		if _, ok := desired[key]; !ok {
			cancel()
			delete(p.active, key)
		}
	}
}

func (p *InstancePoller) run(ctx context.Context, workflowName string, t WorkflowTrigger) {
	defer func() {
		p.mu.Lock()
		delete(p.active, workflowName+":"+t.Topic)
		p.mu.Unlock()
	}()

	group := t.ConsumerGroup
	if group == "" {
		group = fmt.Sprintf("queuetask-trigger-%s", workflowName)
	}

	consumer := p.client.NewConsumer(t.Topic, queueti.WithConsumerGroup(group))

	err := consumer.Consume(ctx, func(msgCtx context.Context, msg *queueti.Message) error {
		payload, err := marshalQueueTiPayload(msg)
		if err != nil {
			return fmt.Errorf("marshalling message payload: %w", err)
		}
		if _, err := p.engine.StartInstance(msgCtx, workflowName, payload); err != nil {
			slog.Warn("instance trigger failed", "workflow", workflowName, "topic", t.Topic, "error", err)
			return err // Nack
		}
		return nil // Ack
	})

	if err != nil && ctx.Err() == nil {
		slog.Warn("instance trigger consumer exited", "workflow", workflowName, "topic", t.Topic, "error", err)
	}
}

// Stop cancels all running goroutines.
func (p *InstancePoller) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, cancel := range p.active {
		cancel()
	}
	p.active = make(map[string]context.CancelFunc)
}
