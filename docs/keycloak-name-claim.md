# Keycloak — Verifying the `name` Claim in ID Token

Knox uses the `name` claim from Keycloak's ID token to display the user's full name in JIT access request tickets. This guide explains how to verify and configure this claim.

## Quick Check: Is `name` Already Included?

By default, Keycloak **includes the `name` claim** in ID tokens for most configurations. To verify:

1. Go to **Keycloak Admin Console** → **Clients** → select your Knox client (e.g., `n8n-proxy`)
2. Go to the **Client Scopes** tab
3. Click on the `profile` scope (should be in **Default** scopes)
4. Go to **Mappers** tab
5. Look for a mapper called "**full name**" or "**name**"

If it exists and is enabled, the `name` claim is already being sent.

## Testing the Token

You can verify the actual token content:

1. Go to **Keycloak Admin Console** → **Clients** → your Knox client
2. Click the **Client Scopes** tab → **Evaluate** sub-tab
3. Select a test user from the dropdown
4. Click **Generated ID Token**
5. Check the JSON output for:

```json
{
  "name": "Omar Midiyanto",
  "preferred_username": "omi01",
  "email": "omar.midiyanto@corp.com",
  ...
}
```

## If `name` Claim is Missing

If the `name` claim is not present, create a mapper:

### Option 1: Use Built-in "Full Name" Mapper

1. Go to **Clients** → your Knox client → **Client Scopes** → `profile` scope
2. Click **Add mapper** → **By configuration**
3. Select **User's full name**
4. Configure:
   - **Name**: `full name`
   - **Token Claim Name**: `name`
   - **Add to ID token**: ✅ ON
   - **Add to access token**: ✅ ON
5. Click **Save**

### Option 2: Manual User Attribute Mapper

If users have first/last name set in Keycloak:

1. Go to **Clients** → your Knox client → **Client Scopes** → `profile` scope
2. Click **Add mapper** → **By configuration**
3. Select **User Attribute**
4. Configure:
   - **Name**: `name`
   - **User Attribute**: `name` (or use `firstName` / `lastName`)
   - **Token Claim Name**: `name`
   - **Claim JSON Type**: `String`
   - **Add to ID token**: ✅ ON
5. Click **Save**

## Knox Fallback Behavior

Knox handles missing `name` claim gracefully with this fallback chain:

```
name claim → preferred_username → email
```

If neither `name` nor `preferred_username` are present, Knox will use the user's email as the display name.

## Ensuring Users Have Names Set

For the `name` claim to contain useful data, users must have their **First Name** and **Last Name** set in Keycloak:

1. Go to **Users** → select a user
2. Ensure **First Name** and **Last Name** are filled in
3. The `name` claim will be: `"First Last"` (e.g., "Omar Midiyanto")

> **Tip**: If your organization uses LDAP/AD federation, these fields are typically synced automatically from the directory.
