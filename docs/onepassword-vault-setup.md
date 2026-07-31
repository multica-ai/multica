# 1Password vault setup for agentrunner workspaces

Any workspace on either agentrunner pipeline (`agentrunner-generator`,
tools/prod; `agentrunner-dev-generator`, dev-server) can layer 1Password items
on top of its SSM-backed secrets. This is opt-in per workspace — nothing here
runs until you set `onepassword_vault` in the workspace's registry entry (step
1).

Mechanism: `gitops/base/agent-runtime/secret-store.yaml` and
`onepassword-external-secret.yaml` (AIPLAT-161).

## Vaults are squad-owned, not created per workspace

Every FY27 squad already has one 1Password vault assigned to it, named after
the squad. Access is Okta-automated: every squad member and the service
account behind `onepassword-squad-vault` already have access — there is no
vault-creation or access-grant step to perform here. (A normal user cannot
grant the service account access to a vault even if they wanted to; that's
IT/Okta-managed.)

Use your squad's existing vault. Don't create a new one.

## Process

1. **Set `onepassword_vault` in the workspace's SSM registry entry**, pointing
   at your squad's vault name:

   - tools/prod pipeline: `/agentfarm/tools/agentrunner/<slug>`
   - dev pipeline: `/agentfarm/tools/agentrunner-dev/<slug>`

   ```json
   { "onepassword_vault": "<squad-vault-name>" }
   ```

   This is what the ArgoCD ApplicationSet's `templatePatch` reads
   (`g2crowd/configuration`). No `onepassword_vault` → the `SecretStore` and
   `ExternalSecret` are deleted outright for that workspace's Application, no
   lookup ever happens. Set it → they're kept, the `SecretStore` is patched to
   point at your squad's vault, and the `ExternalSecret`'s tag search is
   scoped to this workspace's slug.

2. **Create an object (item) in your squad's vault** — web UI or CLI:

   ```sh
   op item create --vault <squad-vault-name> --title <item-name> \
     DATABASE_URL="postgres://..." \
     API_KEY="..."
   ```

3. **Tag it with the workspace's slug.** A given slug tag may be applied to
   only **one** object — don't tag a second item with the same slug.

   ```sh
   op item edit <item-name> --vault <squad-vault-name> --tags <workspace-slug>
   ```

   Or, in the web UI: open the item → add a Tag matching the slug exactly.

4. **Wait ~10 minutes** for the value to be fetched (ArgoCD resync plus the
   `ExternalSecret`'s own refresh cycle).

**Field naming is load-bearing, not cosmetic.** The k8s Secret key is the
1Password field TITLE verbatim — no prefix, no case transform. Field titles
on the object must:

- exactly match the env var you want (`DATABASE_URL`, not `database url`)
- not be duplicated within the object

If a slug ever ends up tagged onto more than one object by mistake, and two
tagged items share a field title, that's a hard ESO error ("found multiple
labels with the same key") — the `ExternalSecret` stops syncing for the whole
workspace until fixed, not just for the colliding field.

**Collision with SSM-backed vars**: this is a second, independent `envFrom`
stacked after the SSM-backed one. If a 1Password field title matches an
existing SSM param name for the same workspace, the 1Password value wins
silently (Kubernetes resolves `envFrom` key collisions by list order, and
1Password is listed second). Check the workspace's existing SSM keys before
naming a field, or you'll shadow one without any error surfacing.

**Planned convenience (post-GA, not built yet):** Gandalf may get an optional
field to specify the squad's vault directly (possibly Okta-derived), removing
the manual SSM registry edit in step 1.

## Verify

```sh
kubectl get externalsecret agentrunner-1password-secrets -n agentrunner-<slug>
kubectl get secretstore onepassword-squad-vault -n agentrunner-<slug> -o yaml
```

`SYNCED` / `READY: True` means it pulled successfully. If the resources don't
exist at all, `onepassword_vault` isn't set (or the ApplicationSet hasn't
resynced yet — check the Application's sync status). If the `ExternalSecret`
exists but has an error condition, check for the duplicate-slug-tag /
field-title collision above.

Pods start fine either way — the `envFrom` reference to
`agentrunner-1password-secrets` is `optional: true`, so a missing or
zero-match Secret never blocks startup.
