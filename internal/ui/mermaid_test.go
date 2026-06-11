package ui

import (
	"strings"
	"testing"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

func TestMermaidEscape(t *testing.T) {
	cases := []struct{ in, want string }{
		{`say "hi"`, `say #quot;hi#quot;`},
		{`#heading`, `#35;heading`},
		{`both "and" #hash`, `both #quot;and#quot; #35;hash`},
		{`normal-step`, `normal-step`},
	}
	for _, c := range cases {
		if got := mermaidEscape(c.in); got != c.want {
			t.Errorf("mermaidEscape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildMermaidDiagram(t *testing.T) {
	tests := []struct {
		name   string
		def    workflow.Definition
		checks []string // substrings that must appear
		absent []string // substrings that must NOT appear
	}{
		{
			name: "empty workflow returns empty string",
			def:  workflow.Definition{Name: "empty"},
		},
		{
			name: "single step no deps",
			def: workflow.Definition{
				Name:  "wf",
				Steps: []workflow.StepDef{{Name: "step-one", Trigger: workflow.TriggerManual}},
			},
			checks: []string{`flowchart TD`, `s0["step-one"]`, `classDef manual`},
		},
		{
			name: "double-quote in step name is escaped",
			def: workflow.Definition{
				Name:  "wf",
				Steps: []workflow.StepDef{{Name: `say "hi"`, Trigger: workflow.TriggerManual}},
			},
			checks: []string{`say #quot;hi#quot;`},
			absent: []string{`"hi"`},
		},
		{
			name: "hash in description is escaped",
			def: workflow.Definition{
				Name:  "wf",
				Steps: []workflow.StepDef{{Name: "s", Description: "#important", Trigger: workflow.TriggerAuto}},
			},
			checks: []string{`#35;important`},
			absent: []string{`"#important"`},
		},
		{
			name: "depends_on edge is emitted",
			def: workflow.Definition{
				Name: "wf",
				Steps: []workflow.StepDef{
					{Name: "a", Trigger: workflow.TriggerManual},
					{Name: "b", Trigger: workflow.TriggerAuto, DependsOn: []string{"a"}},
				},
			},
			checks: []string{`s0 --> s1`},
		},
		{
			name: "trigger shapes",
			def: workflow.Definition{
				Name: "wf",
				Steps: []workflow.StepDef{
					{Name: "m", Trigger: workflow.TriggerManual},
					{Name: "a", Trigger: workflow.TriggerAuto},
					{Name: "q", Trigger: workflow.TriggerQueueTi},
					{Name: "h", Trigger: workflow.TriggerHTTP},
				},
			},
			checks: []string{
				`s0["m"]`,    // manual → rectangle
				`s1(["a"])`,  // auto → stadium
				`s2[("q")]`,  // queueti → cylinder
				`s3[["h"]]`,  // http → subroutine
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMermaidDiagram(&tt.def)
			if len(tt.def.Steps) == 0 {
				if got != "" {
					t.Errorf("expected empty string for no steps, got %q", got)
				}
				return
			}
			for _, want := range tt.checks {
				if !strings.Contains(got, want) {
					t.Errorf("expected diagram to contain %q\ngot:\n%s", want, got)
				}
			}
			for _, sub := range tt.absent {
				if strings.Contains(got, sub) {
					t.Errorf("expected diagram NOT to contain %q\ngot:\n%s", sub, got)
				}
			}
		})
	}
}
