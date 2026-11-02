package core

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

// SynthSchemaV1 is strict as Codex wants it: one object whose only
// property, answer, is a string, required, and no other property allowed.
// It is apart from the judge schema byte for byte.
func TestSynthSchemaV1(t *testing.T) {
	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
		Additional *bool                      `json:"additionalProperties"`
	}
	dec := json.NewDecoder(strings.NewReader(SynthSchemaV1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&schema); err != nil {
		t.Fatalf("the schema is not the expected JSON: %v", err)
	}
	if schema.Type != "object" || len(schema.Properties) != 1 || !slices.Equal(schema.Required, []string{"answer"}) ||
		schema.Additional == nil || *schema.Additional {
		t.Errorf("schema = %+v", schema)
	}
	var answer map[string]any
	if err := json.Unmarshal(schema.Properties["answer"], &answer); err != nil || len(answer) != 1 || answer["type"] != "string" {
		t.Errorf("answer = %s (%v)", schema.Properties["answer"], err)
	}
	if SynthSchemaV1 == JudgeSchemaV1 || strings.Contains(SynthSchemaV1, "judgments") || strings.Contains(JudgeSchemaV1, `"answer"`) {
		t.Error("the synth schema and the judge schema must tell each other apart")
	}
	// Written by hand: no white space, as the adapters compare it byte for
	// byte.
	if strings.ContainsAny(SynthSchemaV1, " \n\t") {
		t.Errorf("schema has white space: %q", SynthSchemaV1)
	}
}

func TestSynthNamesAndLimits(t *testing.T) {
	if !slices.Contains(AllCapabilities, CapSynthesize) || CapSynthesize != "synthesize" {
		t.Errorf("AllCapabilities = %v, want synthesize in it", AllCapabilities)
	}
	if AllCapabilities[len(AllCapabilities)-1] != CapSynthesize {
		t.Errorf("synthesize is added at the end: %v", AllCapabilities)
	}
	if SynthSchemaFile != "synth-schema-v1.json" || SynthSchemaFile == JudgeSchemaFile {
		t.Errorf("SynthSchemaFile = %q", SynthSchemaFile)
	}
	if SynthTimeout != 3*time.Minute {
		t.Errorf("SynthTimeout = %v", SynthTimeout)
	}
}
