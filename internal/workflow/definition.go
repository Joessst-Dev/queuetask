package workflow

import (
	"fmt"
	"net/url"
	"os"
	"strings"

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

type TriggerType string

const (
	TriggerManual  TriggerType = "manual"
	TriggerAuto    TriggerType = "auto"
	TriggerQueueTi TriggerType = "queueti"
	TriggerHTTP    TriggerType = "http"
)

type Definition struct {
	Name        string       `yaml:"name"`
	Version     int          `yaml:"version"`
	Description string       `yaml:"description"`
	Steps       []StepDef    `yaml:"steps"`
}

type HTTPDef struct {
	Method  string            `yaml:"method"`
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
}

type StepDef struct {
	Name               string      `yaml:"name"`
	Description        string      `yaml:"description"`
	Trigger            TriggerType `yaml:"trigger"`
	DependsOn          []string    `yaml:"depends_on"`
	PublishToTopic     string      `yaml:"publish_to_topic"`
	QueueTiTopic       string      `yaml:"queueti_topic"`
	QueueTiConsumerGrp string      `yaml:"queueti_consumer_group"`
	HTTP               *HTTPDef    `yaml:"http"`
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
		if strings.ContainsAny(s.Name, "/ ?#&%+") {
			return fmt.Errorf("step %q name contains URL-unsafe characters (/, space, ?, #, &, %%, +)", s.Name)
		}
		if _, dup := names[s.Name]; dup {
			return fmt.Errorf("duplicate step name %q", s.Name)
		}
		names[s.Name] = struct{}{}

		switch s.Trigger {
		case TriggerManual, TriggerAuto, TriggerQueueTi, TriggerHTTP:
		case "":
			d.Steps[i].Trigger = TriggerManual
		default:
			return fmt.Errorf("step %q has unknown trigger type %q", s.Name, s.Trigger)
		}

		if s.Trigger == TriggerQueueTi && s.QueueTiTopic == "" {
			return fmt.Errorf("step %q with trigger=queueti requires queueti_topic", s.Name)
		}

		if s.Trigger == TriggerHTTP {
			if s.HTTP == nil || s.HTTP.URL == "" {
				return fmt.Errorf("step %q with trigger=http requires http.url", s.Name)
			}
			if err := validateHTTPURL(s.HTTP.URL); err != nil {
				return fmt.Errorf("step %q http.url: %w", s.Name, err)
			}
			if s.HTTP.Method != "" {
				method := strings.ToUpper(s.HTTP.Method)
				if !validHTTPMethods[method] {
					return fmt.Errorf("step %q http.method %q is not a recognized HTTP method", s.Name, s.HTTP.Method)
				}
				d.Steps[i].HTTP.Method = method
			}
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
