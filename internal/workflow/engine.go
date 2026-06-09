package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/Joessst-Dev/queuetask/internal/publisher"
)

type Engine struct {
	repo      *Repository
	registry  *Registry
	publisher publisher.Publisher
	poller    *Poller
}

func NewEngine(repo *Repository, registry *Registry, pub publisher.Publisher, poller *Poller) *Engine {
	return &Engine{repo: repo, registry: registry, publisher: pub, poller: poller}
}

// StartInstance creates a new workflow instance and kicks off the first steps.
func (e *Engine) StartInstance(ctx context.Context, workflowName string, input json.RawMessage) (*Instance, error) {
	def, ok := e.registry.Get(workflowName)
	if !ok {
		return nil, fmt.Errorf("workflow %q not found", workflowName)
	}

	inst, err := e.repo.CreateInstance(ctx, workflowName, input)
	if err != nil {
		return nil, err
	}

	if err := e.repo.CreateSteps(ctx, inst.ID, def.Steps); err != nil {
		return nil, err
	}

	if err := e.advance(ctx, inst.ID); err != nil {
		return nil, err
	}

	return e.repo.GetInstance(ctx, inst.ID)
}

// TriggerStep advances a waiting_manual step and then advances the workflow.
func (e *Engine) TriggerStep(ctx context.Context, instanceID uuid.UUID, stepName string, output json.RawMessage) error {
	step, err := e.repo.GetStep(ctx, instanceID, stepName)
	if err != nil {
		return err
	}

	if step.Status != StatusWaitingManual && step.Status != StatusWaitingQueueTi {
		return fmt.Errorf("step %q is not waiting (status: %s)", stepName, step.Status)
	}

	if err := e.completeStep(ctx, instanceID, stepName, output); err != nil {
		return err
	}

	return e.advance(ctx, instanceID)
}

func (e *Engine) completeStep(ctx context.Context, instanceID uuid.UUID, stepName string, output json.RawMessage) error {
	step, err := e.repo.GetStep(ctx, instanceID, stepName)
	if err != nil {
		return err
	}

	if err := e.repo.UpdateStepStatus(ctx, instanceID, stepName, StatusCompleted, output, ""); err != nil {
		return err
	}

	if step.PublishTopic != "" && len(output) > 0 {
		if pubErr := e.publisher.Publish(step.PublishTopic, output); pubErr != nil {
			// Non-fatal: log but don't fail the step
			fmt.Printf("warn: publishing to topic %s: %v\n", step.PublishTopic, pubErr)
		}
	}
	return nil
}

// advance inspects all pending steps and activates those whose dependencies are met.
func (e *Engine) advance(ctx context.Context, instanceID uuid.UUID) error {
	steps, err := e.repo.ListSteps(ctx, instanceID)
	if err != nil {
		return err
	}

	completedSet := make(map[string]json.RawMessage, len(steps))
	for _, s := range steps {
		if s.Status == StatusCompleted {
			completedSet[s.StepName] = s.Output
		}
	}

	allDone := true
	anyFailed := false
	for _, s := range steps {
		if s.Status == StatusFailed {
			anyFailed = true
		}
		if s.Status != StatusCompleted && s.Status != StatusFailed {
			allDone = false
		}
	}

	if anyFailed {
		return e.repo.UpdateInstanceStatus(ctx, instanceID, InstanceFailed)
	}
	if allDone {
		return e.repo.UpdateInstanceStatus(ctx, instanceID, InstanceCompleted)
	}

	for _, s := range steps {
		if s.Status != StatusPending {
			continue
		}
		if !depsCompleted(s.DependsOn, completedSet) {
			continue
		}

		// Merge outputs from dependencies as this step's input
		mergedInput := mergeOutputs(s.DependsOn, completedSet)

		switch s.TriggerType {
		case TriggerManual:
			if err := e.repo.UpdateStepStatus(ctx, instanceID, s.StepName, StatusWaitingManual, mergedInput, ""); err != nil {
				return err
			}

		case TriggerAuto:
			if err := e.repo.UpdateStepStatus(ctx, instanceID, s.StepName, StatusRunning, mergedInput, ""); err != nil {
				return err
			}
			if err := e.completeStep(ctx, instanceID, s.StepName, mergedInput); err != nil {
				return err
			}
			// Recurse to activate any newly unblocked steps
			return e.advance(ctx, instanceID)

		case TriggerQueueTi:
			if err := e.repo.UpdateStepStatus(ctx, instanceID, s.StepName, StatusWaitingQueueTi, mergedInput, ""); err != nil {
				return err
			}
			if e.poller != nil {
				e.poller.Watch(instanceID, s)
			}
		}
	}

	return nil
}

func depsCompleted(deps []string, completed map[string]json.RawMessage) bool {
	for _, d := range deps {
		if _, ok := completed[d]; !ok {
			return false
		}
	}
	return true
}

// mergeOutputs builds a JSON object from the outputs of the listed dependencies.
func mergeOutputs(deps []string, outputs map[string]json.RawMessage) json.RawMessage {
	if len(deps) == 0 {
		return nil
	}
	merged := make(map[string]json.RawMessage, len(deps))
	for _, d := range deps {
		if out, ok := outputs[d]; ok && len(out) > 0 {
			merged[d] = out
		}
	}
	if len(merged) == 0 {
		return nil
	}
	b, _ := json.Marshal(merged)
	return b
}
