# Knox JIT Ticketing API Reference

This document outlines the API endpoints available in the Knox JIT (Just-In-Time) Ticketing system. 

The API is divided into two sections:
1. **User APIs**: Used by the browser frontend (requires an active Knox user session).
2. **Admin APIs**: Used by external tools, integrations (like an n8n workflow), or administrators. Protected by an API key.

---

## 1. User APIs

These endpoints are protected by the Knox `Auth` middleware. They require a valid session cookie (automatically handled by the browser when logged into Knox). All actions are scoped to the authenticated user.

### `POST /knox-api/request-jit`
Submit a new JIT access request.

**Request Body (JSON):**
```json
{
  "workflow_id": "AUqnl095YhDTo47d",
  "access_type": ["run", "edit"], 
  "period_start": "2026-04-14T10:00:00+07:00",
  "period_end": "2026-04-17T10:00:00+07:00",
  "description": "Need to debug production sync workflow"
}
```
*Note: `access_type` must be an array containing `"run"`, `"edit"`, or both.*

**Response (201 Created):**
```json
{
  "ticket": {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "ticket_number": "JIT-20260414-A3F8",
    "email": "omar.midiyanto@corp.com",
    "display_name": "Omar Midiyanto",
    "team": "finance",
    "workflow_id": "AUqnl095YhDTo47d",
    "access_type": "run,edit",
    "description": "Need to debug production sync workflow",
    "period_start": "2026-04-14T03:00:00Z",
    "period_end": "2026-04-17T03:00:00Z",
    "status": "requested",
    "created_at": "2026-04-14T01:30:00Z",
    "updated_at": "2026-04-14T01:30:00Z"
  },
  "duration": "3d 0h 0m"
}
```

### `GET /knox-api/tickets`
List tickets belonging to the currently logged-in user.

**Query Parameters (Optional):**
- `status`: Filter by status (e.g., `requested`, `active`, `expired`)
- `limit`: Maximum number of results (default: `50`, max: `100`)

**Response (200 OK):**
```json
{
  "tickets": [
    {
      "id": "...",
      "ticket_number": "JIT-20260414-A3F8",
      "status": "active",
      ...
    }
  ],
  "total": 1
}
```

### `GET /knox-api/tickets/{id}`
Get the details of a specific ticket. The user can only view their own tickets.

**Response (200 OK):**
```json
{
  "ticket": { ... },
  "duration": "3d 0h 0m"
}
```

### `GET /knox-api/user-info`
Get the current user's profile information as resolved by Knox from Keycloak.

**Response (200 OK):**
```json
{
  "email": "omar.midiyanto@corp.com",
  "display_name": "Omar Midiyanto",
  "team": "finance"
}
```

---

## 2. Admin APIs

These endpoints are intended for machine-to-machine communication, such as an external approval system or an n8n webhook workflow.

**Authentication:** 
Must include the API Key defined in the `KNOX_API_KEY` environment variable.
Pass the key via the header: 
`X-Knox-API-Key: your_api_key_here`
*(Alternatively, generic `Authorization: Bearer your_api_key_here` is also supported).*

### `GET /knox-api/admin/tickets`
List all tickets in the system, with optional filtering.

**Query Parameters:**
- `email`: Filter by requestor email
- `team`: Filter by n8n team
- `status`: Filter by status
- `workflow_id`: Filter by workflow ID
- `limit`: Pagination limit (default: `50`, max: `200`)
- `offset`: Pagination offset (default: `0`)

**Example Request:**
```bash
curl -X GET "https://knox.yourdomain.com/knox-api/admin/tickets?status=requested&team=finance" \
     -H "X-Knox-API-Key: my-secret-key"
```

**Response (200 OK):**
```json
{
  "tickets": [ ... ],
  "total": 5
}
```

### `GET /knox-api/admin/tickets/{id}`
Get the details of any ticket in the system by its UUID.

**Example Request:**
```bash
curl -X GET "https://knox.yourdomain.com/knox-api/admin/tickets/a1b2c3d4-e5f6-7890-abcd-ef1234567890" \
     -H "X-Knox-API-Key: my-secret-key"
```

### `PATCH /knox-api/admin/tickets/{id}/status`
Update the status of a ticket. This is typically used by an approval workflow to approve or reject a ticket.

**Valid Transitions:**
- `requested` → `approved` or `rejected`
- `approved` → `revoked`
- `active` → `revoked`

*(Note: Transitions to `active` and `expired` are handled automatically by Knox in the background based on the `period_start` and `period_end` dates).*

**Request Body (JSON):**
```json
{
  "status": "approved",
  "reason": "Approved by engineering lead via Slack",
  "updated_by": "admin-bot"
}
```

**Example Request:**
```bash
curl -X PATCH "https://knox.yourdomain.com/knox-api/admin/tickets/a1b2c3d4-e5f6-7890-abcd-ef1234567890/status" \
     -H "X-Knox-API-Key: my-secret-key" \
     -H "Content-Type: application/json" \
     -d '{
           "status": "approved",
           "reason": "Looks good",
           "updated_by": "Jira-Integration"
         }'
```

**Response (200 OK):**
```json
{
  "ticket": {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "status": "approved",
    "status_reason": "Looks good",
    "updated_by": "Jira-Integration",
    "updated_at": "2026-04-14T02:00:00Z",
    ...
  },
  "duration": "3d 0h 0m"
}
```

---

## The JIT Webhook Notification Payload

If `JIT_WEBHOOK_URL` is configured, Knox will send an HTTP `POST` to that URL asynchronously immediately after a **User** successfully submits a new request (`/knox-api/request-jit`). 

The webhook receiver (e.g., an n8n webhook node) is expected to parse this payload and trigger an approval flow (e.g., send a Slack message with Approve/Reject buttons that call the Admin API).

**Webhook Payload Format:**
```json
{
  "ticket_id":        "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "ticket_number":    "JIT-20260414-A3F8",
  "user_requestor":   "omar.midiyanto@corp.com",
  "display_name":     "Omar Midiyanto",
  "n8n_team":         "finance",
  "workflow_id":      "AUqnl095YhDTo47d",
  "access_type":      "run,edit",
  "description":      "Need to debug production sync workflow",
  "jit_period_start": "2026-04-14T03:00:00Z",
  "jit_period_end":   "2026-04-17T03:00:00Z",
  "jit_duration":     "3d 0h 0m",
  "requested_at":     "2026-04-14T01:30:00Z",
  "status":           "requested"
}
```