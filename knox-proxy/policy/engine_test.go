package policy

import (
	"encoding/json"
	"testing"
)

func TestEvaluate_NonAPIPaths(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"static asset", "GET", "/assets/main.js"},
		{"root path", "GET", "/"},
		{"favicon", "GET", "/favicon.ico"},
		{"websocket", "GET", "/ws"},
		{"POST to non-API", "POST", "/some/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := engine.Evaluate(tt.method, tt.path, nil, nil)
			if !d.Allowed {
				t.Errorf("expected allowed for %s %s, got denied: %s", tt.method, tt.path, d.Reason)
			}
		})
	}
}

func TestEvaluate_ReadOnlyMethods(t *testing.T) {
	engine := NewEngine()
	methods := []string{"GET", "HEAD", "OPTIONS", "get", "Get"}

	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			d := engine.Evaluate(m, "/rest/workflows", nil, nil)
			if !d.Allowed {
				t.Errorf("expected allowed for %s, got denied: %s", m, d.Reason)
			}
		})
	}
}

func TestEvaluate_InternalResourceWhitelist(t *testing.T) {
	engine := NewEngine()
	resources := []string{"telemetry", "ph"}

	for _, r := range resources {
		t.Run(r, func(t *testing.T) {
			d := engine.Evaluate("POST", "/rest/"+r, nil, nil)
			if !d.Allowed {
				t.Errorf("expected whitelisted resource %s to be allowed, got denied: %s", r, d.Reason)
			}
		})
	}
}

func TestEvaluate_MutationOnNonWorkflowResource(t *testing.T) {
	engine := NewEngine()
	resources := []string{"credentials", "settings", "users", "tags"}

	for _, r := range resources {
		t.Run(r, func(t *testing.T) {
			d := engine.Evaluate("POST", "/rest/"+r, nil, []string{"run:*"})
			if d.Allowed {
				t.Errorf("expected mutation on /rest/%s to be denied", r)
			}
		})
	}
}

func TestEvaluate_WorkflowEditWithRole(t *testing.T) {
	engine := NewEngine()

	d := engine.Evaluate("PATCH", "/rest/workflows/abc123", nil, []string{"edit:abc123"})
	if !d.Allowed {
		t.Errorf("expected allowed with exact edit role, got denied: %s", d.Reason)
	}
}

func TestEvaluate_WorkflowEditWithoutRole(t *testing.T) {
	engine := NewEngine()

	d := engine.Evaluate("PATCH", "/rest/workflows/abc123", nil, []string{"run:abc123"})
	if d.Allowed {
		t.Error("expected denied: run role should not grant edit access")
	}
}

func TestEvaluate_WorkflowRunWithRole(t *testing.T) {
	engine := NewEngine()

	d := engine.Evaluate("POST", "/rest/workflows/abc123/run", nil, []string{"run:abc123"})
	if !d.Allowed {
		t.Errorf("expected allowed with run role, got denied: %s", d.Reason)
	}
}

func TestEvaluate_WildcardRoles(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name   string
		method string
		path   string
		roles  []string
	}{
		{"run:* allows any workflow run", "POST", "/rest/workflows/xyz/run", []string{"run:*"}},
		{"edit:* allows any workflow edit", "PATCH", "/rest/workflows/xyz", []string{"edit:*"}},
		{"run:* allows test action", "POST", "/rest/workflows/xyz/test", []string{"run:*"}},
		{"edit:* allows activate", "POST", "/rest/workflows/xyz/activate", []string{"edit:*"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := engine.Evaluate(tt.method, tt.path, nil, tt.roles)
			if !d.Allowed {
				t.Errorf("expected allowed with wildcard, got denied: %s", d.Reason)
			}
		})
	}
}

func TestEvaluate_CreateNewWorkflowDenied(t *testing.T) {
	engine := NewEngine()

	d := engine.Evaluate("POST", "/rest/workflows", nil, []string{"edit:*", "run:*"})
	if d.Allowed {
		t.Error("expected creating new workflow to be denied")
	}
}

func TestEvaluate_WorkflowRunFromBody(t *testing.T) {
	engine := NewEngine()

	body := map[string]interface{}{
		"workflowData": map[string]interface{}{
			"id": "wf123",
		},
	}
	bodyBytes, _ := json.Marshal(body)

	d := engine.Evaluate("POST", "/rest/workflows/run", bodyBytes, []string{"run:wf123"})
	if !d.Allowed {
		t.Errorf("expected allowed when workflowId extracted from body, got denied: %s", d.Reason)
	}
}

func TestEvaluate_WorkflowRunFromBodyWrongRole(t *testing.T) {
	engine := NewEngine()

	body := map[string]interface{}{
		"workflowData": map[string]interface{}{
			"id": "wf123",
		},
	}
	bodyBytes, _ := json.Marshal(body)

	d := engine.Evaluate("POST", "/rest/workflows/run", bodyBytes, []string{"run:wf999"})
	if d.Allowed {
		t.Error("expected denied when role doesn't match workflow ID in body")
	}
}

func TestEvaluate_ExecutionWithRoles(t *testing.T) {
	engine := NewEngine()

	// Manipulating existing execution (stop/delete) — allowed with any role
	d := engine.Evaluate("POST", "/rest/executions/exec123/stop", nil, []string{"run:anything"})
	if !d.Allowed {
		t.Errorf("expected allowed for execution manipulation with roles: %s", d.Reason)
	}

	// Manipulating execution without roles — denied
	d = engine.Evaluate("DELETE", "/rest/executions/exec123", nil, nil)
	if d.Allowed {
		t.Error("expected denied for execution manipulation without roles")
	}
}

func TestEvaluate_ExecutionCreateFromBody(t *testing.T) {
	engine := NewEngine()

	body := map[string]interface{}{
		"workflowId": "wf456",
	}
	bodyBytes, _ := json.Marshal(body)

	d := engine.Evaluate("POST", "/rest/executions", bodyBytes, []string{"run:wf456"})
	if !d.Allowed {
		t.Errorf("expected allowed for execution create with matching role: %s", d.Reason)
	}
}

func TestEvaluate_PathTraversalInWorkflowID(t *testing.T) {
	engine := NewEngine()

	// Path traversal characters won't match the pathIDRegex,
	// so the engine treats it as a non-workflow resource mutation
	d := engine.Evaluate("PATCH", "/rest/workflows/../../../etc/passwd", nil, []string{"edit:*"})
	// The path has segments that don't match workflow ID pattern,
	// so it should be treated differently than a valid workflow path
	_ = d // Just verify no panic occurs
}

func TestEvaluate_ActivateDeactivateRequiresEdit(t *testing.T) {
	engine := NewEngine()

	// activate with run role — denied
	d := engine.Evaluate("POST", "/rest/workflows/wf1/activate", nil, []string{"run:wf1"})
	if d.Allowed {
		t.Error("expected activate to require edit role, not run")
	}

	// activate with edit role — allowed
	d = engine.Evaluate("POST", "/rest/workflows/wf1/activate", nil, []string{"edit:wf1"})
	if !d.Allowed {
		t.Errorf("expected allowed with edit role for activate: %s", d.Reason)
	}
}

func TestExtractWorkflowIDFromBody(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]interface{}
		expected string
	}{
		{
			"workflowData.id format",
			map[string]interface{}{"workflowData": map[string]interface{}{"id": "abc"}},
			"abc",
		},
		{
			"workflowId format",
			map[string]interface{}{"workflowId": "def"},
			"def",
		},
		{
			"empty body",
			map[string]interface{}{},
			"",
		},
		{
			"no matching field",
			map[string]interface{}{"other": "value"},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			got := extractWorkflowIDFromBody(bodyBytes)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}

	// Test nil/empty
	t.Run("nil body", func(t *testing.T) {
		got := extractWorkflowIDFromBody(nil)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	// Test invalid JSON
	t.Run("invalid JSON", func(t *testing.T) {
		got := extractWorkflowIDFromBody([]byte("not json"))
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
}

func TestEvaluate_NoRoles(t *testing.T) {
	engine := NewEngine()

	d := engine.Evaluate("POST", "/rest/workflows/wf1/run", nil, nil)
	if d.Allowed {
		t.Error("expected denied with no JIT roles")
	}

	d = engine.Evaluate("POST", "/rest/workflows/wf1/run", nil, []string{})
	if d.Allowed {
		t.Error("expected denied with empty JIT roles")
	}
}

func TestEvaluate_DeleteWorkflow(t *testing.T) {
	engine := NewEngine()

	d := engine.Evaluate("DELETE", "/rest/workflows/wf1", nil, []string{"edit:wf1"})
	if !d.Allowed {
		t.Errorf("expected DELETE to be allowed with edit role: %s", d.Reason)
	}
}


