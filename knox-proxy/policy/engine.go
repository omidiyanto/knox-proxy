package policy

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// pathIDRegex validates that a workflow ID contains alphanumeric characters, dashes, and underscores.
// This prevents path traversal attacks via crafted IDs.
var pathIDRegex = regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)

// internalResourceWhitelist contains n8n-internal REST sub-resources that n8n's
// own UI posts to as part of normal operation (telemetry, analytics, feature flags).
// These are whitelisted to prevent log spam and unnecessary 403 errors.
// They carry no workflow data and present no security risk from KNOX's perspective.
var internalResourceWhitelist = map[string]bool{
	"telemetry": true, // n8n telemetry proxy → external analytics service
	"ph":        true, // PostHog analytics proxy → feature flags & usage tracking
}

// Decision represents the result of a policy evaluation.
type Decision struct {
	Allowed bool
	Reason  string
}

// Engine evaluates API access policies based on JIT (Just-In-Time) roles.
// Default policy: all HTTP methods except GET/HEAD/OPTIONS are denied
// unless the user has a matching JIT role assigned in Keycloak.
type Engine struct{}

// NewEngine creates a new JIT policy engine.
func NewEngine() *Engine {
	return &Engine{}
}

// Evaluate checks if the given HTTP method and path are allowed based on JIT roles.
//
// Rules:
//   - Non-API paths (UI assets, etc.): ALLOW
//   - GET/HEAD/OPTIONS: ALLOW (read-only)
//   - Internal n8n resources (telemetry, ph): ALLOW (whitelisted)
//   - POST/PATCH/PUT/DELETE on /rest/workflows/*: Check JIT roles
//   - All other mutations: DENY
func (e *Engine) Evaluate(method, urlPath string, reqBody []byte, jitRoles []string) Decision {
	// Normalize path to prevent traversal
	cleanPath := path.Clean(urlPath)

	// Non-REST paths are allowed (UI assets, WebSocket, static files, etc.)
	if !strings.HasPrefix(cleanPath, "/rest/") && !strings.HasPrefix(cleanPath, "/api/") {
		return Decision{Allowed: true, Reason: "non-API path"}
	}

	// Read-only methods are always allowed
	method = strings.ToUpper(method)
	if method == "GET" || method == "HEAD" || method == "OPTIONS" {
		return Decision{Allowed: true, Reason: "read-only method"}
	}

	// For mutation methods, evaluate JIT rules
	return e.evaluateMutation(method, cleanPath, reqBody, jitRoles)
}

// extractWorkflowIDFromBody attempts to parse the workflow ID from the JSON body.
// Handles patterns like `{"workflowData": {"id": "123"}}` or `{"workflowId": "123"}`.
func extractWorkflowIDFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}

	// Try format {"workflowData": {"id": "..."}} (used in manual executions)
	if wdRaw, ok := payload["workflowData"]; ok {
		if wd, ok := wdRaw.(map[string]interface{}); ok {
			if id, ok := wd["id"].(string); ok && id != "" {
				return id
			}
		}
	}

	// Try format {"workflowId": "..."} (used in some execution endpoints)
	if id, ok := payload["workflowId"].(string); ok && id != "" {
		return id
	}

	return ""
}

// evaluateMutation checks if a mutation request is authorized by JIT roles.
func (e *Engine) evaluateMutation(method, cleanPath string, reqBody []byte, jitRoles []string) Decision {
	parts := strings.Split(strings.TrimPrefix(cleanPath, "/"), "/")

	if len(parts) < 2 {
		return Decision{Allowed: false, Reason: "mutation on unknown API path"}
	}

	resource := parts[1] // e.g. "workflows", "executions", "telemetry", "ph"

	// Allow mutations to internal n8n resources that the UI posts to automatically.
	// These are proxied by n8n to external analytics services and are harmless.
	if internalResourceWhitelist[resource] {
		return Decision{Allowed: true, Reason: fmt.Sprintf("internal resource '%s' whitelisted", resource)}
	}

	// Only workflows and executions are allowed to be mutated via JIT
	if resource != "workflows" && resource != "executions" {
		return Decision{
			Allowed: false,
			Reason:  fmt.Sprintf("mutation on /rest/%s is not allowed", resource),
		}
	}

	var workflowID string
	var requiredAction string

	if resource == "executions" {
		// Execution manipulation endpoints:
		//   POST   /rest/executions          → create execution (body contains workflowId)
		//   POST   /rest/executions/<id>/stop → stop a running execution
		//   DELETE /rest/executions/<id>      → delete an execution record

		if len(parts) >= 3 {
			// Manipulating an existing execution (stop/delete).
			// KNOX cannot map an execution ID to its parent workflow ID in real-time
			// without an additional n8n API call. Allow as long as the user has at
			// least one valid JIT role (i.e., they are a legitimate n8n operator).
			if len(jitRoles) > 0 {
				return Decision{
					Allowed: true,
					Reason:  "execution manipulation allowed — user holds at least one JIT role",
				}
			}
			return Decision{Allowed: false, Reason: "cannot manipulate executions without any JIT roles"}
		}

		// Generic POST /rest/executions — extract workflow ID from request body
		workflowID = extractWorkflowIDFromBody(reqBody)
		if workflowID == "" {
			return Decision{Allowed: false, Reason: "cannot determine workflow ID from executions payload"}
		}
		requiredAction = "run"

	} else if resource == "workflows" {
		if len(parts) == 3 && parts[2] == "run" {
			// POST /rest/workflows/run — manual trigger (workflow ID in body)
			workflowID = extractWorkflowIDFromBody(reqBody)
			if workflowID == "" {
				return Decision{Allowed: false, Reason: "cannot determine workflow ID from manual run payload (workflow not saved?)"}
			}
			requiredAction = "run"
		} else if len(parts) < 3 {
			// POST /rest/workflows — creating a new workflow
			return Decision{
				Allowed: false,
				Reason:  "creating new workflows is not allowed via JIT",
			}
		} else {
			workflowID = parts[2]
			if len(parts) >= 4 {
				switch parts[3] {
				case "run", "test":
					requiredAction = "run"
				case "activate", "deactivate":
					requiredAction = "edit" // activate/deactivate requires edit permission
				default:
					requiredAction = "edit"
				}
			} else {
				// PATCH or DELETE /rest/workflows/<id>
				requiredAction = "edit"
			}
		}
	}

	// Validate workflow ID format to prevent path traversal attacks
	if !pathIDRegex.MatchString(workflowID) {
		return Decision{
			Allowed: false,
			Reason:  fmt.Sprintf("invalid workflow ID format: %s", workflowID),
		}
	}

	// Check if user holds the required JIT role
	requiredRole := fmt.Sprintf("%s:%s", requiredAction, workflowID)
	for _, role := range jitRoles {
		// Exact match: "run:fQbVF5ChXltsHTnR"
		if role == requiredRole {
			return Decision{
				Allowed: true,
				Reason:  fmt.Sprintf("JIT role '%s' matched", requiredRole),
			}
		}
		// Wildcard match: "run:*" grants access to all workflows for that action
		wildcardRole := fmt.Sprintf("%s:*", requiredAction)
		if role == wildcardRole {
			return Decision{
				Allowed: true,
				Reason:  fmt.Sprintf("JIT wildcard role '%s' matched", wildcardRole),
			}
		}
	}

	return Decision{
		Allowed: false,
		Reason:  fmt.Sprintf("missing JIT role '%s' — request denied", requiredRole),
	}
}
