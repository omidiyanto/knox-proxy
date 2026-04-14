# Keycloak Setup Guide for KNOX Project

This document explains the Keycloak configuration steps required for the KNOX IAM Gateway to function correctly.

---

## Prerequisites

- Keycloak is running and accessible (e.g., `https://sso.company.com`)
- Admin access to the Keycloak Administration Console
- A Realm is already created (e.g., `corp`)

---

## 1. Creating an OIDC Client

1. Login to the **Keycloak Admin Console**
2. Navigate to **Clients** → **Create client**
3. Fill in the form:

   | Field | Value |
   |---|---|
   | Client type | OpenID Connect |
   | Client ID | `n8n-proxy` |
   | Name | KNOX IAM Gateway |
   | Description | n8n CE Identity & Access Management Proxy |

4. Click **Next**
5. On the **Capability config** page:
   - **Client authentication**: ✅ ON (confidential)
   - **Authorization**: OFF
   - **Standard flow**: ✅ ON
   - **Direct access grants**: OFF

6. Click **Next**
7. On the **Login settings** page:

   | Field | Value |
   |---|---|
   | Root URL | `http://localhost:8443` |
   | Home URL | `http://localhost:8443` |
   | Valid redirect URIs | `http://localhost:8443/auth/callback` |
   | Valid post logout redirect URIs | `http://localhost:8443/auth/callback` |
   | Web origins | `http://localhost:8443` |

   > **Note:** Adjust the URLs to match your production domain (e.g., `https://n8n.company.com`)

8. Click **Save**

### Obtaining the Client Secret

1. Open the `n8n-proxy` client
2. Go to the **Credentials** tab
3. Copy the **Client secret** value → this is your `OIDC_CLIENT_SECRET`

---

## 2. Enabling Backchannel Logout

This feature allows Keycloak Admins to instantly terminate user sessions in KNOX.

1. Open the `n8n-proxy` client
2. Go to the **Settings** tab → scroll down to the **Logout settings** section
3. Configure the following:

   | Field | Value |
   |---|---|
   | Backchannel logout URL | `http://knox:8443/auth/backchannel-logout` |
   | Backchannel logout session required | ✅ ON |
   | Backchannel logout revoke offline sessions | ✅ ON |
   | Front channel logout | OFF |

   > **IMPORTANT:** The `http://knox:8443` URL uses the internal Docker container name. 
   > If Keycloak is running outside the Docker network, use a reachable IP/hostname.

4. Click **Save**

---

## 3. Adding a Group Mapper (for Teams)

For the JWT token to contain the user's `groups` information, we need to add a mapper:

1. Open the `n8n-proxy` client
2. Go to the **Client scopes** tab → click `n8n-proxy-dedicated`
3. Click **Add mapper** → **By configuration** → select **Group Membership**
4. Fill in the form:

   | Field | Value |
   |---|---|
   | Name | `groups` |
   | Token Claim Name | `groups` |
   | Full group path | ✅ ON |
   | Add to ID token | ✅ ON |
   | Add to access token | ✅ ON |
   | Add to userinfo | ✅ ON |

5. Click **Save**

---

## 4. Creating Groups (Teams)

Groups represent Teams/Departments. The group name format must use the prefix configured in KNOX (default: `n8n-prod-`).

1. Navigate to **Groups** → **Create group**
2. Create groups according to your teams:

   | Group Name | Description |
   |---|---|
   | `n8n-prod-finance` | Finance Team — will be mapped to the `fin@local` n8n account |
   | `n8n-prod-developer` | Developer Team — will be mapped to the `dev@local` n8n account |
   | `n8n-prod-operations` | Operations Team (optional) |

   > **Note:** The name after the `n8n-prod-` prefix must **exactly match** the key in `N8N_TEAM_CREDENTIALS`.
   > For example: group `n8n-prod-finance` → key `"finance"` in the JSON credentials.

---

## 5. Creating Client Roles (JIT PAM)

Client Roles are used to grant temporary access to specific workflows.

1. Open the `n8n-proxy` client
2. Go to the **Roles** tab → **Create role**
3. Role naming format:

   | Role Name | Allowed Actions |
   |---|---|
   | `run:<workflow_id>` | Execute a specific workflow |
   | `edit:<workflow_id>` | Edit a specific workflow |
   | `run:*` | Execute all workflows (wildcard) |
   | `edit:*` | Edit all workflows (wildcard) |

   **Example:**
   ```
   run:oPCazLQ12jH8mpMf       → Can run the workflow oPCazLQ12jH8mpMf
   edit:oPCazLQ12jH8mpMf      → Can edit the workflow oPCazLQ12jH8mpMf
   run:* → Can run all workflows
   ```

   > **How to get the Workflow ID:** Open the workflow in n8n and check the URL bar.
   > Example: `https://n8n.example.com/workflow/oPCazLQ12jH8mpMf` → ID: `oPCazLQ12jH8mpMf`

---

## 6. Creating Users and Assigning Groups + Roles

### Creating a User

1. Navigate to **Users** → **Add user**
2. Fill in the user data:

   | Field | Value |
   |---|---|
   | Username | `andi.finance` |
   | Email | `andi@company.com` |
   | Email verified | ✅ ON |
   | First name | Andi |
   | Last name | Finance |

3. Click **Create**
4. Go to the **Credentials** tab → **Set password** → enter the password → Set **Temporary** to OFF

### Assigning a User to a Group

1. Open the user you just created
2. Go to the **Groups** tab → **Join group**
3. Select the appropriate group (e.g., `n8n-prod-finance`)

### Assigning JIT Roles to a User

1. Open the user
2. Go to the **Role mapping** tab → **Assign role**
3. In the filter, select **Filter by clients** → search for `n8n-proxy`
4. Check the desired role (e.g., `run:oPCazLQ12jH8mpMf`)
5. Click **Assign**

   > **JIT Concept:** Assign this role only when the user needs access (troubleshooting).
   > Once finished, **Remove** the role to revoke access.
   > If the user is currently logged in, use **Sessions** → **Sign out** to trigger a backchannel logout.

---

## 7. Terminating User Sessions (Instant Revocation)

To immediately revoke a user's access (e.g., after JIT is completed):

1. Navigate to **Users** → search for the user
2. Go to the **Sessions** tab
3. Click **Sign out** on the active session

Keycloak will send a backchannel logout request to KNOX, and the KNOX session will be instantly destroyed.
The user will be prompted to log in again on their next request.

### Alternative: Log Out All Users from the Client

1. Open the `n8n-proxy` client
2. Go to the **Sessions** tab
3. Click **Sign out all** → all KNOX sessions tied to this client will be removed

---

## 8. Creating Local n8n Accounts (Per Team)

Each team requires a local account in n8n, which KNOX will use for Identity Multiplexing.

1. Access n8n directly via `http://server:5678`
2. Login as an Admin
3. Create the user/accounts:

   | Email | Password | Description |
   |---|---|---|
   | `fin@local` | `financePwd123` | Shared account for the Finance Team |
   | `dev@local` | `devPwd456` | Shared account for the Developer Team |

   > These credentials must **exactly match** the `N8N_TEAM_CREDENTIALS` found in your `.env` file.

4. Create an example workflow in each account for testing purposes.

---

## 9. Verifying the Configuration

### Test OIDC Discovery
```bash
curl -s [https://sso.company.com/realms/corp/.well-known/openid-configuration](https://sso.company.com/realms/corp/.well-known/openid-configuration) | jq .
```

### Test Token (Resource Owner Password — for debugging only)
```bash
curl -s -X POST \
  [https://sso.company.com/realms/corp/protocol/openid-connect/token](https://sso.company.com/realms/corp/protocol/openid-connect/token) \
  -d "client_id=n8n-proxy" \
  -d "client_secret=YOUR_SECRET" \
  -d "grant_type=password" \
  -d "username=andi.finance" \
  -d "password=userPassword" \
  -d "scope=openid email groups" | jq .access_token -r | cut -d. -f2 | base64 -d 2>/dev/null | jq .
```

Verify that the JWT output contains:
```json
{
  "email": "andi@company.com",
  "groups": ["/n8n-prod-finance"],
  "resource_access": {
    "n8n-proxy": {
      "roles": ["run:oPCazLQ12jH8mpMf"]
    }
  },
  "sid": "keycloak-session-id-here"
}
```

### Test Backchannel Logout Endpoint
```bash
# Ensure KNOX is accessible from Keycloak
curl -v http://knox:8443/auth/backchannel-logout
# Expect: 405 Method Not Allowed (because it must be a POST request)
```

### Full Flow Test
1. Open your browser to: `http://localhost:8443`
2. You will be redirected to the Keycloak login page
3. Login as `andi.finance`
4. After the callback, you will enter the n8n UI as the Finance team
5. Try accessing a workflow → you should only have READ access
6. Assign the `run:<id>` role in Keycloak → refresh the page → you can now RUN it
7. In Keycloak Admin → Users → andi → Sessions → Sign out
8. Refresh your browser → you will be redirected to the login page (the session is dead)

---

## Troubleshooting

### "No team assignment found"
- Ensure the user is assigned to the correct group.
- Ensure the group name uses the correct prefix (`/n8n-prod-`).
- Check the `KEYCLOAK_GROUP_PREFIX` environment variable in KNOX.

### "Missing JIT role"
- Ensure the role format is correct: `run:<id>` or `edit:<id>`.
- Ensure the role is assigned under **Client Roles** (not Realm Roles).
- Ensure the workflow ID is correct (double-check the n8n URL bar).

### Backchannel Logout is Not Working
- Ensure the **Backchannel logout URL** in Keycloak points to the correct KNOX endpoint.
- Ensure Keycloak can reach KNOX over the network (check your Docker network).
- Check the KNOX logs for signature verification errors.
- Ensure **Backchannel logout session required** is set to ON.

### Cookie Pre-warm Failed
- Ensure the local n8n accounts have been created and the credentials match.
- Ensure n8n is running before KNOX starts (check `depends_on` in your docker-compose file).
- Check the KNOX logs for: `Failed to pre-warm cookie`.