# KNOX — Identity & Access Management Gateway for n8n Community Edition

KNOX is a security-hardened IAM Reverse Proxy designed explicitly for **n8n Community Edition (CE)**. It transforms n8n CE into a team-aware, enterprise-ready automation platform by providing OIDC authentication, Just-In-Time (JIT) access control, and Identity Multiplexing.

## Key Features

- 🔐 **OIDC Authentication**: Native integration with Keycloak (or any OIDC provider).
- 👥 **Identity Multiplexing**: Maps multiple OIDC users/groups to distinct local n8n accounts (Team/Department isolation).
- 🎟️ **JIT Ticketing System**: Fully-featured ticket management with PostgreSQL backend and webhook notifications for automated approval flows.
- ⚡ **JIT Access Control**: Granular, role-based access for specific workflows (`run:<id>`, `edit:<id>`) via Keycloak Client Roles.
- 🛡️ **Security Sanitization**: Prevents execution of restricted nodes and sanitizes request payloads.
- 🎨 **UI Hardening**: Intercepts UI to inject a custom, air-gapped-ready JIT request modal and sidebar menu.
- 🔌 **Backchannel Logout**: Instant session revocation support via Keycloak.
- 📡 **WebSocket Optimization**: Tuned for n8n's real-time push events and high-concurrency execution monitoring.

## Architecture Flow

```
User (Browser)
    │
    │ HTTPS/HTTP
    ▼
╔══════════════════════════════════════╗
║  KNOX (Port 8443)                    ║
║                                      ║
║  1. OIDC Middleware                  ║
║     → Validate session               ║
║     → Or redirect to Keycloak        ║
║                                      ║
║  2. Policy Middleware                ║
║     → Default Deny mutations         ║
║     → Check JIT roles (run/edit)     ║
║     → Sanitize body (if enabled)     ║
║                                      ║
║  3. JIT Ticketing API                ║
║     → POST /knox-api/request         ║
║     → Update knox-db & fire webhook  ║
║                                      ║
║  4. Identity Multiplexer             ║
║     → Resolve team from groups       ║
║     → Get n8n cookie from pool       ║
║     → Strip client cookies           ║
║     → Inject team n8n-auth cookie    ║
╚════════════════│═════════════════════╝
                 │ Internal network
                 ▼
╔══════════════════════════════════════╗
║  Nginx (Port 80, internal only)      ║
║  → CSS injection (hide UI elements)  ║
║  → JS injection (logout button)      ║
║  → Webhook blocking                  ║
╚════════════════│═════════════════════╝
                 │
                 ▼
╔══════════════════════════════════════╗
║  n8n CE (Port 5678)                  ║
║  → Receives authenticated request    ║
║  → Executes in team's workspace      ║
╚══════════════════════════════════════╝

╔══════════════════════════════════════╗
║  PostgreSQL (Port 5432)              ║
║  → Persistent JIT ticket storage     ║
╚══════════════════════════════════════╝
```

## Prerequisites

- **Docker & Docker Compose**
- **Keycloak** (or similar OIDC provider)
- **n8n Community Edition** (v2.x)

## Quick Start

1. **Configure Environment**: Copy `.env.example` to `.env` and fill in your OIDC and n8n credentials.
2. **Keycloak Setup**: Follow the [Keycloak Setup Guide](docs/keycloak-setup-guide.md) to configure your client mappers and roles.
3. **Deploy**:
   ```bash
   docker compose up -d
   ```

## Configuration (Environment Variables)

| Variable | Description | Default |
|----------|-------------|---------|
| `OIDC_ISSUER_URL` | Your OIDC provider issuer URL | - |
| `OIDC_CLIENT_ID` | OIDC Client ID | `n8n-proxy` |
| `OIDC_CLIENT_SECRET` | OIDC Client Secret | - |
| `KEYCLOAK_GROUP_PREFIX` | Prefix used to identify team groups | `/n8n-prod-` |
| `N8N_TEAM_CREDENTIALS` | JSON mapping of teams to n8n accounts | - |
| `JIT_WEBHOOK_URL` | Webhook URL for ticket notifications | - |
| `KNOX_API_KEY` | Key to protect `/knox-api/admin/*` endpoints | - |
| `DANGEROUS_NODES` | Comma-separated list of restricted nodes | - |

## Security Model

### Just-In-Time (JIT) Roles
KNOX enforces access based on specific Client Roles assigned in Keycloak:
- `run:<workflow_id>`: Allows manual execution of the specified workflow.
- `edit:<workflow_id>`: Allows modification of the specified workflow.
- `run:*` / `edit:*`: Wildcard permissions for all workflows.

### JIT Ticketing System
To allow users to organically request these Keycloak roles:
1. Nginx injects a "Request Access" modal directly into the n8n UI sidebar.
2. Form submissions securely hit Knox's Go backend.
3. Knox enriches the payload with true Keycloak user identity (avoiding CORS/spoofing risks).
4. Tickets are saved persistantly to `knox-db` (PostgreSQL).
5. An automated webhook is fired to an external aggregator (like an n8n workflow or Jira) for Slack approval flows.
6. Admin tools can query and patch ticket statuses through the API key-protected `/knox-api/admin/tickets` endpoints.

### Default Deny
By default, KNOX blocks all mutating requests (POST, PUT, DELETE) unless the user has explicitly been granted an appropriate role for the target resource.


