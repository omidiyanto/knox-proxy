package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizer_AllowAllMode(t *testing.T) {
	s := NewSanitizer(nil, "")

	if s.IsEnabled() {
		t.Error("expected sanitizer to be disabled with no dangerous nodes")
	}

	body := []byte(`{"nodes":[{"type":"n8n-nodes-base.executeCommand","name":"Shell"}]}`)
	if err := s.SanitizeWorkflowBody(body); err != nil {
		t.Errorf("expected no error in allow-all mode, got: %v", err)
	}
}

func TestSanitizer_BlocksDangerousNode(t *testing.T) {
	s := NewSanitizer([]string{"n8n-nodes-base.executeCommand"}, "")

	if !s.IsEnabled() {
		t.Error("expected sanitizer to be enabled")
	}

	body := []byte(`{"nodes":[{"type":"n8n-nodes-base.executeCommand","name":"Shell Command"}]}`)
	err := s.SanitizeWorkflowBody(body)
	if err == nil {
		t.Error("expected error for dangerous node, got nil")
	}
}

func TestSanitizer_SafeWorkflow(t *testing.T) {
	s := NewSanitizer([]string{"n8n-nodes-base.executeCommand"}, "")

	body := []byte(`{"nodes":[{"type":"n8n-nodes-base.httpRequest","name":"HTTP Request"}]}`)
	err := s.SanitizeWorkflowBody(body)
	if err != nil {
		t.Errorf("expected no error for safe workflow, got: %v", err)
	}
}

func TestSanitizer_MultipleDangerousNodes(t *testing.T) {
	s := NewSanitizer([]string{
		"n8n-nodes-base.executeCommand",
		"n8n-nodes-base.ssh",
		"n8n-nodes-base.code",
	}, "")

	nodes := s.ListDangerousNodes()
	if len(nodes) != 3 {
		t.Errorf("expected 3 dangerous nodes, got %d", len(nodes))
	}

	body := []byte(`{"nodes":[{"type":"n8n-nodes-base.ssh","name":"SSH"}]}`)
	err := s.SanitizeWorkflowBody(body)
	if err == nil {
		t.Error("expected error for ssh node")
	}
}

func TestSanitizer_InvalidJSON(t *testing.T) {
	s := NewSanitizer([]string{"n8n-nodes-base.executeCommand"}, "")

	err := s.SanitizeWorkflowBody([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSanitizer_NoNodesField(t *testing.T) {
	s := NewSanitizer([]string{"n8n-nodes-base.executeCommand"}, "")

	body := []byte(`{"name":"My Workflow"}`)
	err := s.SanitizeWorkflowBody(body)
	if err != nil {
		t.Errorf("expected no error when nodes field missing, got: %v", err)
	}
}

func TestSanitizer_EmptyNodesArray(t *testing.T) {
	s := NewSanitizer([]string{"n8n-nodes-base.executeCommand"}, "")

	body := []byte(`{"nodes":[]}`)
	err := s.SanitizeWorkflowBody(body)
	if err != nil {
		t.Errorf("expected no error for empty nodes array, got: %v", err)
	}
}

func TestSanitizer_FileLoading(t *testing.T) {
	// Create temp YAML config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "dangerous-nodes.yaml")

	config := DangerousNodesConfig{
		Nodes: []string{"n8n-nodes-base.readWriteFile", "n8n-nodes-base.function"},
	}
	data, _ := json.Marshal(config)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	s := NewSanitizer(nil, configPath)

	if !s.IsEnabled() {
		t.Error("expected sanitizer to be enabled from file")
	}

	nodes := s.ListDangerousNodes()
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes from file, got %d", len(nodes))
	}
}

func TestSanitizer_CombinedSources(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "dangerous-nodes.yaml")

	config := DangerousNodesConfig{
		Nodes: []string{"n8n-nodes-base.readWriteFile"},
	}
	data, _ := json.Marshal(config)
	os.WriteFile(configPath, data, 0644)

	s := NewSanitizer([]string{"n8n-nodes-base.executeCommand"}, configPath)

	nodes := s.ListDangerousNodes()
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes from combined sources, got %d", len(nodes))
	}
}

func TestSanitizer_WhitespaceHandling(t *testing.T) {
	s := NewSanitizer([]string{"  n8n-nodes-base.code  ", "", "  "}, "")

	nodes := s.ListDangerousNodes()
	if len(nodes) != 1 {
		t.Errorf("expected 1 node after trimming, got %d", len(nodes))
	}
}

func TestSanitizer_NonExistentFile(t *testing.T) {
	// Should not panic, just log warning
	s := NewSanitizer(nil, "/nonexistent/path.yaml")
	if s.IsEnabled() {
		t.Error("expected disabled with nonexistent file")
	}
}
