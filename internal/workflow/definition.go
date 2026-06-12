// Package workflow implements the workflow execution engine, step definitions,
// and persistent state management for queuetask workflow instances.
package workflow

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

var validHTTPMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "HEAD": true, "OPTIONS": true,
}

// validateHTTPURL checks that a URL has an http/https scheme and a non-empty host.
// Private/loopback IP ranges are blocked at execution time by the engine.
func validateHTTPURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("must use http or https scheme")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("missing host")
	}
	return nil
}

// TriggerType identifies how a step is advanced.
type TriggerType string

const (
	TriggerManual  TriggerType = "manual"  // waits for a human trigger via the API
	TriggerAuto    TriggerType = "auto"    // completes immediately with merged dependency output
	TriggerQueueTi TriggerType = "queueti" // waits for a queue-ti message
	TriggerHTTP    TriggerType = "http"    // makes an outbound HTTP request
)

// InstanceTriggerType identifies how a new workflow instance is created automatically.
type InstanceTriggerType string

const (
	InstanceTriggerCron    InstanceTriggerType = "cron"    // schedule-based; requires schedule field
	InstanceTriggerWebhook InstanceTriggerType = "webhook" // POST /api/v1/workflows/:name/webhook
	InstanceTriggerQueueTi InstanceTriggerType = "queueti" // message on a queue-ti topic; requires topic field
)

// WorkflowTrigger defines an automatic instance-creation trigger.
// The fields that apply depend on Type:
//   - cron: Schedule (required), Input (optional)
//   - webhook: no additional fields
//   - queueti: Topic (required), ConsumerGroup (optional)
type WorkflowTrigger struct {
	Type          InstanceTriggerType `yaml:"type"`
	Schedule      string              `yaml:"schedule"`       // cron: standard 5-field cron expression
	Input         any                 `yaml:"input"`          // cron: static JSON passed as instance input
	Topic         string              `yaml:"topic"`          // queueti: topic to consume
	ConsumerGroup string              `yaml:"consumer_group"` // queueti: consumer group name
}

// WorkflowNotification declares which lifecycle events trigger notifications
// and who to contact. On accepts the event type strings defined in the notify
// package (e.g. "instance.completed", "step.waiting_manual").
type WorkflowNotification struct {
	On    []string           `yaml:"on"`
	Email *NotifyEmailTarget `yaml:"email"`
	SMS   *NotifySMSTarget   `yaml:"sms"`
}

// NotifyEmailTarget lists the email addresses to receive notifications.
type NotifyEmailTarget struct {
	To []string `yaml:"to"`
}

// NotifySMSTarget lists the phone numbers (E.164) to receive SMS notifications.
type NotifySMSTarget struct {
	To []string `yaml:"to"`
}

// Definition is the in-memory representation of a parsed and validated workflow
// YAML file. It is stored in the Registry and referenced by the engine at
// instance-creation time.
type Definition struct {
	Name          string                `yaml:"name"`
	Version       int                   `yaml:"version"`
	Description   string                `yaml:"description"`
	Triggers      []WorkflowTrigger     `yaml:"triggers"`
	Steps         []StepDef             `yaml:"steps"`
	Notifications *WorkflowNotification `yaml:"notifications"`
}

// HTTPDef holds the configuration for an http-trigger step.
// URL must use http or https; private and loopback IP ranges are blocked at
// execution time by the engine's SSRF filter. Method defaults to POST when empty.
type HTTPDef struct {
	Method  string            `yaml:"method"`
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
}

// StepDef describes one step in a workflow. Steps activate when all DependsOn
// steps have completed. A step with no DependsOn activates when the instance is
// created. If Input is non-nil it overrides the merged dependency outputs.
type StepDef struct {
	Name               string      `yaml:"name"`
	Description        string      `yaml:"description"`
	Trigger            TriggerType `yaml:"trigger"`
	DependsOn          []string    `yaml:"depends_on"`
	Input              any         `yaml:"input,omitempty"`
	PublishToTopic     string      `yaml:"publish_to_topic"`
	QueueTiTopic       string      `yaml:"queueti_topic"`
	QueueTiConsumerGrp string      `yaml:"queueti_consumer_group"`
	HTTP               *HTTPDef    `yaml:"http"`
}

const (
	urlUnsafeChars     = "/ ?#&%+"
	urlUnsafeCharsDesc = "/, space, ?, #, &, %, +"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func (d *Definition) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("workflow name is required")
	}
	if err := validateTriggers(d.Triggers); err != nil {
		return err
	}
	names, err := validateSteps(d.Steps)
	if err != nil {
		return err
	}
	return validateStepDependencies(d.Steps, names)
}

func validateTriggers(triggers []WorkflowTrigger) error {
	for i, t := range triggers {
		switch t.Type {
		case InstanceTriggerCron:
			if t.Schedule == "" {
				return fmt.Errorf("trigger[%d]: cron trigger requires a schedule", i)
			}
			if _, err := cronParser.Parse(t.Schedule); err != nil {
				return fmt.Errorf("trigger[%d]: invalid cron schedule %q: %w", i, t.Schedule, err)
			}
		case InstanceTriggerWebhook:
			// no required fields
		case InstanceTriggerQueueTi:
			if t.Topic == "" {
				return fmt.Errorf("trigger[%d]: queueti trigger requires topic", i)
			}
		case "":
			return fmt.Errorf("trigger[%d]: type is required", i)
		default:
			return fmt.Errorf("trigger[%d]: unknown trigger type %q", i, t.Type)
		}
	}
	return nil
}

// validateSteps checks each step definition and normalises default values in place.
// It returns the set of step names for use in dependency validation.
func validateSteps(steps []StepDef) (map[string]struct{}, error) {
	names := make(map[string]struct{}, len(steps))
	for i, s := range steps {
		if s.Name == "" {
			return nil, fmt.Errorf("step[%d] name is required", i)
		}
		if strings.ContainsAny(s.Name, urlUnsafeChars) {
			return nil, fmt.Errorf("step %q name contains URL-unsafe characters (%s)", s.Name, urlUnsafeCharsDesc)
		}
		if _, dup := names[s.Name]; dup {
			return nil, fmt.Errorf("duplicate step name %q", s.Name)
		}
		names[s.Name] = struct{}{}

		switch s.Trigger {
		case TriggerManual, TriggerAuto, TriggerQueueTi, TriggerHTTP:
		case "":
			steps[i].Trigger = TriggerManual
		default:
			return nil, fmt.Errorf("step %q has unknown trigger type %q", s.Name, s.Trigger)
		}

		if s.Trigger == TriggerQueueTi && s.QueueTiTopic == "" {
			return nil, fmt.Errorf("step %q with trigger=queueti requires queueti_topic", s.Name)
		}

		if s.Trigger == TriggerHTTP {
			if s.HTTP == nil || s.HTTP.URL == "" {
				return nil, fmt.Errorf("step %q with trigger=http requires http.url", s.Name)
			}
			if err := validateHTTPURL(s.HTTP.URL); err != nil {
				return nil, fmt.Errorf("step %q http.url: %w", s.Name, err)
			}
			if s.HTTP.Method != "" {
				method := strings.ToUpper(s.HTTP.Method)
				if !validHTTPMethods[method] {
					return nil, fmt.Errorf("step %q http.method %q is not a recognized HTTP method", s.Name, s.HTTP.Method)
				}
				steps[i].HTTP.Method = method
			}
		}
	}
	return names, nil
}

func validateStepDependencies(steps []StepDef, names map[string]struct{}) error {
	for _, s := range steps {
		for _, dep := range s.DependsOn {
			if _, ok := names[dep]; !ok {
				return fmt.Errorf("step %q depends on unknown step %q", s.Name, dep)
			}
		}
	}
	return nil
}

// HasTriggerType reports whether the definition declares at least one instance
// trigger of the given type.
func (d *Definition) HasTriggerType(t InstanceTriggerType) bool {
	for _, trigger := range d.Triggers {
		if trigger.Type == t {
			return true
		}
	}
	return false
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
