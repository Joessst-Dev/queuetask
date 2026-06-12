package notify

import (
	"bytes"
	"fmt"
	"text/template"
)

var (
	emailSubjectTmpl = template.Must(template.New("email_subject").Parse(
		`[queuetask] {{.Event}} — {{.WorkflowName}}`,
	))

	emailBodyTmpl = template.Must(template.New("email_body").Parse(
		`Workflow:  {{.WorkflowName}}
Instance:  {{.InstanceID}}
Step:      {{.StepName}}
Event:     {{.Event}}
Timestamp: {{.Timestamp}}
`,
	))

	smsTmpl = template.Must(template.New("sms").Parse(
		`[queuetask] {{.Event}}: {{.WorkflowName}}{{if .StepName}} / {{.StepName}}{{end}} at {{.Timestamp}}`,
	))
)

type templateData struct {
	WorkflowName string
	InstanceID   string
	StepName     string
	Event        string
	Timestamp    string
}

func renderTemplates(e Event) (subject, emailBody, smsBody string, err error) {
	data := templateData{
		WorkflowName: e.WorkflowName,
		InstanceID:   e.InstanceID.String(),
		StepName:     e.StepName,
		Event:        string(e.Type),
		Timestamp:    e.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
	}

	var buf bytes.Buffer

	if err = emailSubjectTmpl.Execute(&buf, data); err != nil {
		return "", "", "", fmt.Errorf("rendering email subject: %w", err)
	}
	subject = buf.String()
	buf.Reset()

	if err = emailBodyTmpl.Execute(&buf, data); err != nil {
		return "", "", "", fmt.Errorf("rendering email body: %w", err)
	}
	emailBody = buf.String()
	buf.Reset()

	if err = smsTmpl.Execute(&buf, data); err != nil {
		return "", "", "", fmt.Errorf("rendering sms body: %w", err)
	}
	smsBody = buf.String()

	return subject, emailBody, smsBody, nil
}
