package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Joessst-Dev/queuetask/internal/publisher"
)

const (
	httpDefaultTimeout  = 30 * time.Second
	httpMaxResponseSize = 10 * 1024 * 1024 // 10 MiB
)

func mustParseCIDR(cidr string) *net.IPNet {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(fmt.Sprintf("invalid CIDR %q: %v", cidr, err))
	}
	return n
}

// blockedCIDRs is the set of IP ranges HTTP steps must not target.
// NOTE: DNS rebinding (a hostname that resolves to a private IP) is not prevented here;
// that requires a custom Dialer that re-checks post-resolution.
var blockedCIDRs = []*net.IPNet{
	mustParseCIDR("127.0.0.0/8"),    // IPv4 loopback
	mustParseCIDR("::1/128"),        // IPv6 loopback
	mustParseCIDR("169.254.0.0/16"), // link-local / AWS IMDS
	mustParseCIDR("fe80::/10"),      // IPv6 link-local
	mustParseCIDR("10.0.0.0/8"),     // RFC 1918
	mustParseCIDR("172.16.0.0/12"),  // RFC 1918
	mustParseCIDR("192.168.0.0/16"), // RFC 1918
	mustParseCIDR("0.0.0.0/8"),      // "this" network
	mustParseCIDR("100.64.0.0/10"),  // RFC 6598 CGNAT
	mustParseCIDR("fc00::/7"),       // IPv6 ULA
}

// checkSSRFHost returns an error when host is a literal IP address in a blocked range.
func checkSSRFHost(host string) error {
	ip := net.ParseIP(host)
	if ip == nil {
		return nil // hostname — DNS rebinding is a known limitation
	}
	for _, cidr := range blockedCIDRs {
		if cidr.Contains(ip) {
			return fmt.Errorf("host %s is in a blocked network range", host)
		}
	}
	return nil
}

func ssrfRedirectCheck(req *http.Request, _ []*http.Request) error {
	if err := checkSSRFHost(req.URL.Hostname()); err != nil {
		return fmt.Errorf("redirect blocked: %w", err)
	}
	return nil
}

// HTTPDoer is the subset of *http.Client used by the engine, allowing injection in tests.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Engine struct {
	repo        *Repository
	registry    *Registry
	publisher   publisher.Publisher
	poller      *Poller
	httpClient  HTTPDoer
	ssrfEnabled bool
}

func NewEngine(repo *Repository, registry *Registry, pub publisher.Publisher, poller *Poller) *Engine {
	return &Engine{
		repo:      repo,
		registry:  registry,
		publisher: pub,
		poller:    poller,
		httpClient: &http.Client{
			Timeout:       httpDefaultTimeout,
			CheckRedirect: ssrfRedirectCheck,
		},
		ssrfEnabled: true,
	}
}

// SetHTTPClient replaces the HTTP client used for http-trigger steps.
// Calling this disables the built-in SSRF filter, so it should only be used in tests.
func (e *Engine) SetHTTPClient(c HTTPDoer) {
	e.httpClient = c
	e.ssrfEnabled = false
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
			slog.Warn("publishing to topic failed", "topic", step.PublishTopic, "error", pubErr)
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

	completedSet := buildCompletedSet(steps)
	allDone, anyFailed := workflowState(steps)

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
			return e.advance(ctx, instanceID)

		case TriggerQueueTi:
			if err := e.repo.UpdateStepStatus(ctx, instanceID, s.StepName, StatusWaitingQueueTi, mergedInput, ""); err != nil {
				return err
			}
			if e.poller != nil {
				e.poller.Watch(instanceID, s)
			}

		case TriggerHTTP:
			if err := e.repo.UpdateStepStatus(ctx, instanceID, s.StepName, StatusRunning, mergedInput, ""); err != nil {
				return err
			}
			output, httpErr := e.executeHTTPStep(ctx, s, mergedInput)
			if httpErr != nil {
				// Use a detached context: the inbound ctx may already be cancelled
				// (e.g. client disconnect), which would prevent marking the step failed.
				cleanupCtx := context.Background()
				if err := e.repo.UpdateStepStatus(cleanupCtx, instanceID, s.StepName, StatusFailed, nil, httpErr.Error()); err != nil {
					return err
				}
				return e.advance(cleanupCtx, instanceID)
			}
			if err := e.completeStep(ctx, instanceID, s.StepName, output); err != nil {
				return err
			}
			return e.advance(ctx, instanceID)
		}
	}

	return nil
}

func buildCompletedSet(steps []*StepExecution) map[string]json.RawMessage {
	m := make(map[string]json.RawMessage, len(steps))
	for _, s := range steps {
		if s.Status == StatusCompleted {
			m[s.StepName] = s.Output
		}
	}
	return m
}

func workflowState(steps []*StepExecution) (allDone, anyFailed bool) {
	allDone = true
	for _, s := range steps {
		if s.Status == StatusFailed {
			anyFailed = true
		}
		if s.Status != StatusCompleted && s.Status != StatusFailed {
			allDone = false
		}
	}
	return
}

func (e *Engine) executeHTTPStep(ctx context.Context, step *StepExecution, body json.RawMessage) (json.RawMessage, error) {
	req, err := e.buildHTTPRequest(ctx, step, body)
	if err != nil {
		return nil, err
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()
	return parseHTTPResponseBody(resp)
}

func (e *Engine) buildHTTPRequest(ctx context.Context, step *StepExecution, body json.RawMessage) (*http.Request, error) {
	method := step.HTTPMethod
	if method == "" {
		method = http.MethodPost
	}
	bodyAllowed := method != http.MethodGet && method != http.MethodHead

	var reqBody io.Reader
	if bodyAllowed && len(body) > 0 {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, step.HTTPURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	// SSRF pre-flight: block literal private/loopback IP targets.
	if e.ssrfEnabled {
		if err := checkSSRFHost(req.URL.Hostname()); err != nil {
			return nil, fmt.Errorf("SSRF blocked: %w", err)
		}
	}

	if bodyAllowed && len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range step.HTTPHeaders {
		req.Header.Set(k, v)
	}
	return req, nil
}

// parseHTTPResponseBody validates the status code, enforces the size limit,
// and returns the body as JSON (wrapping plain text if needed).
func parseHTTPResponseBody(resp *http.Response) (json.RawMessage, error) {
	// Check status before buffering the full body to avoid reading large error payloads.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, errBody)
	}

	// Read one byte past the limit so we can distinguish "exactly at limit" from "over limit".
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, httpMaxResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	if int64(len(respBody)) > httpMaxResponseSize {
		return nil, fmt.Errorf("response body exceeded %d-byte limit", httpMaxResponseSize)
	}
	if len(respBody) == 0 {
		return nil, nil
	}
	if json.Valid(respBody) {
		return json.RawMessage(respBody), nil
	}
	wrapped, _ := json.Marshal(map[string]string{"body": string(respBody)})
	return json.RawMessage(wrapped), nil
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
