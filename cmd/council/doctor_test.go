package main

import (
	"reflect"
	"testing"
)

func TestAgentExtraEnvKeys(t *testing.T) {
	tests := []struct {
		name   string
		global map[string]string
		agent  map[string]string
		want   []string
	}{
		{
			name:  "nil global, per-agent keys are all extra and sorted",
			agent: map[string]string{"OPENAI_BASE_URL": "x", "ANTHROPIC_BASE_URL": "y"},
			want:  []string{"ANTHROPIC_BASE_URL", "OPENAI_BASE_URL"},
		},
		{
			name:   "keys shared with global at the same value are not extra",
			global: map[string]string{"SHARED": "1"},
			agent:  map[string]string{"SHARED": "1", "ONLY": "2"},
			want:   []string{"ONLY"},
		},
		{
			name:   "an override of a global key counts as extra",
			global: map[string]string{"SHARED": "1"},
			agent:  map[string]string{"SHARED": "2"},
			want:   []string{"SHARED"},
		},
		{
			name:   "no extras returns empty",
			global: map[string]string{"A": "1"},
			agent:  map[string]string{"A": "1"},
			want:   nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := agentExtraEnvKeys(tc.global, tc.agent)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("agentExtraEnvKeys() = %v, want %v", got, tc.want)
			}
		})
	}
}
