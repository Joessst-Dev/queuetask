package workflow

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type TriggerType string

const (
	TriggerManual  TriggerType = "manual"
	TriggerAuto    TriggerType = "auto"
	TriggerQueueTi TriggerType = "queueti"
)

type Definition struct {
	Name        string       `yaml:"name"`
	Version     int          `yaml:"version"`
	Description string       `yaml:"description"`
	Steps       []StepDef    `yaml:"steps"`
}

type StepDef struct {
	Name               string      `yaml:"name"`
	Description        string      `yaml:"description"`
	Trigger            TriggerType `yaml:"trigger"`
	DependsOn          []string    `yaml:"depends_on"`
	PublishToTopic     string      `yaml:"publish_to_topic"`
	QueueTiTopic       string      `yaml:"queueti_topic"`
	QueueTiConsumerGrp string      `yaml:"queueti_consumer_group"`
}

func (d *Definition) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("workflow name is required")
	}
	names := make(map[string]struct{}, len(d.Steps))
	for i, s := range d.Steps {
		if s.Name == "" {
			return fmt.Errorf("step[%d] name is required", i)
		}
		if _, dup := names[s.Name]; dup {
			return fmt.Errorf("duplicate step name %q", s.Name)
		}
		names[s.Name] = struct{}{}

		switch s.Trigger {
		case TriggerManual, TriggerAuto, TriggerQueueTi:
		case "":
			d.Steps[i].Trigger = TriggerManual
		default:
			return fmt.Errorf("step %q has unknown trigger type %q", s.Name, s.Trigger)
		}

		if s.Trigger == TriggerQueueTi && s.QueueTiTopic == "" {
			return fmt.Errorf("step %q with trigger=queueti requires queueti_topic", s.Name)
		}
	}

	for _, s := range d.Steps {
		for _, dep := range s.DependsOn {
			if _, ok := names[dep]; !ok {
				return fmt.Errorf("step %q depends on unknown step %q", s.Name, dep)
			}
		}
	}
	return nil
}

func ParseFile(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := def.Validate(); err != nil {
		return nil, fmt.Errorf("validating %s: %w", path, err)
	}
	return &def, nil
}
