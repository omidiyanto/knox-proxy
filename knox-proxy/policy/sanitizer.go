package policy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Sanitizer checks request bodies for dangerous n8n node types.
// Default behavior: if no dangerous nodes are configured, ALL nodes are allowed.
// Dangerous nodes can be configured via:
//   - DANGEROUS_NODES environment variable (comma-separated)
//   - DANGEROUS_NODES_FILE environment variable (path to YAML/JSON file)
type Sanitizer struct {
	dangerousNodes map[string]bool
}

// DangerousNodesConfig represents the YAML/JSON config file format.
type DangerousNodesConfig struct {
	Nodes []string `json:"nodes" yaml:"nodes"`
}

// NewSanitizer creates a new Sanitizer with the given configuration.
// If no dangerous nodes are provided, all nodes are allowed (default allow-all).
func NewSanitizer(nodes []string, configFile string) *Sanitizer {
	s := &Sanitizer{
		dangerousNodes: make(map[string]bool),
	}

	// Load from inline list (DANGEROUS_NODES env var)
	for _, n := range nodes {
		n = strings.TrimSpace(n)
		if n != "" {
			s.dangerousNodes[n] = true
		}
	}

	// Load from config file (DANGEROUS_NODES_FILE env var)
	if configFile != "" {
		s.loadFromFile(configFile)
	}

	if len(s.dangerousNodes) > 0 {
		slog.Info("Sanitizer initialized with blocklist",
			"dangerous_nodes_count", len(s.dangerousNodes),
			"nodes", s.ListDangerousNodes(),
		)
	} else {
		slog.Info("Sanitizer initialized in allow-all mode (no dangerous nodes configured)")
	}

	return s
}

// loadFromFile loads dangerous node definitions from a YAML or JSON file.
func (s *Sanitizer) loadFromFile(filePath string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		slog.Warn("Failed to read dangerous nodes file", "path", filePath, "error", err)
		return
	}

	var config DangerousNodesConfig

	// Try YAML first (YAML is a superset of JSON)
	if err := yaml.Unmarshal(data, &config); err != nil {
		// Fallback to JSON
		if err := json.Unmarshal(data, &config); err != nil {
			slog.Warn("Failed to parse dangerous nodes file (tried YAML and JSON)",
				"path", filePath, "error", err)
			return
		}
	}

	for _, n := range config.Nodes {
		n = strings.TrimSpace(n)
		if n != "" {
			s.dangerousNodes[n] = true
		}
	}

	slog.Info("Loaded dangerous nodes from file", "path", filePath, "count", len(config.Nodes))
}

// ListDangerousNodes returns the list of configured dangerous node types.
func (s *Sanitizer) ListDangerousNodes() []string {
	nodes := make([]string, 0, len(s.dangerousNodes))
	for n := range s.dangerousNodes {
		nodes = append(nodes, n)
	}
	return nodes
}

// IsEnabled returns true if any dangerous nodes are configured.
// When disabled, SanitizeWorkflowBody is a no-op.
func (s *Sanitizer) IsEnabled() bool {
	return len(s.dangerousNodes) > 0
}

// SanitizeWorkflowBody inspects the JSON body for dangerous n8n node types.
// Returns an error if any dangerous node type is found in the workflow definition.
// Returns nil if the body is safe or if the sanitizer is disabled.
func (s *Sanitizer) SanitizeWorkflowBody(body []byte) error {
	if !s.IsEnabled() {
		return nil // Allow-all mode
	}

	// Parse JSON body
	var workflow map[string]interface{}
	if err := json.Unmarshal(body, &workflow); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}

	// Check the "nodes" array in the workflow definition
	nodesRaw, ok := workflow["nodes"]
	if !ok {
		return nil // No nodes field — safe
	}

	nodesList, ok := nodesRaw.([]interface{})
	if !ok {
		return nil
	}

	for _, nodeRaw := range nodesList {
		node, ok := nodeRaw.(map[string]interface{})
		if !ok {
			continue
		}

		nodeType, ok := node["type"].(string)
		if !ok {
			continue
		}

		if s.dangerousNodes[nodeType] {
			nodeName, _ := node["name"].(string)
			return fmt.Errorf("dangerous node type '%s' (name: '%s') is blocked by security policy",
				nodeType, nodeName)
		}
	}

	return nil
}
