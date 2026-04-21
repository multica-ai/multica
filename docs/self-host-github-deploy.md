# Self-Host Deploy From GitHub

This fork includes a manual GitHub Actions workflow for deploying to the current Ubuntu self-host server.

## What it does

- Triggered manually from GitHub Actions via `Deploy Self-Host`
- Creates a `git bundle` from the selected ref
- Uploads the bundle and backup script to the server over SSH
- Runs a full pre-deploy backup on the server before touching the worktree
- Resets `/home/ubuntu/services/multica` to the bundled commit
- Preserves `.env`
- Rebuilds and restarts the Docker Compose stack
- Verifies `http://127.0.0.1:8080/health` and the frontend on port `3000`

This workflow intentionally uses `workflow_dispatch` only.

The current production server worktree had many local modifications before this GitHub deployment path was added. Manual triggering is safer than auto-deploying every push until the deployment source of truth is fully cleaned up.

## Required GitHub Secrets

Add these repository secrets in `ninggc/multica`:

- `DEPLOY_HOST`
  - Current value: `101.35.232.18`
- `DEPLOY_SSH_USER`
  - Current value: `louis.ning`
- `DEPLOY_SSH_KEY`
  - Private key content for the SSH key that can log into `louis.ning@101.35.232.18`
- `DEPLOY_SSH_PORT`
  - Optional. Default is `22`
- `DEPLOY_KNOWN_HOSTS`
  - Required. Add the exact SSH host key lines for the deployment host.
  - Current value for this server:
    ```text
    101.35.232.18 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAljUxNzXIO3XGcUWz0VfDkhXvXYrfUTD0sEwEt4lZ73
    101.35.232.18 ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQCpL4egB5QAtQ2+nvZXQfDAfOe0igeI6qg/kn+jSxip7nlkN8gtE/Tobrx5adc4Ti3D1MJtyRk9IG+4Brd2gcy5Wbrzd1FuOHk6FcsHx2zLIB13Nk56BDeq0/T/TTJx5m+mxWKVZf1w425/cXesCT6e/H3ju74jwaVPbRAFQhxOSzH5dKBZGGrCrGLNayc6Ef+fF3TAn2pJGwRH0qYivTBHTlWl+h03cSQ9gTepsBtNNmyTn/KznsgkOODSlgQMFlBf9JmQi2IP6RTZ53i8J0cEm8ZqwcUZKx0RTHxnCKVlRYexHtu2RcCdg545lTHskxkrQM/39IwyKDDSfEgQS/T6hf6lF/odRJPMhtlAVAiVSovpTTucyzlqBucXuXBraWc9ZkVlZvrtkkeYIWXTVt9eOX4QRkjtG+w+1VWIBA+A+/Cx4eAPmFbTqDh9gNBZbjKlSoB9MaMkOFbWlyzoEyi7K9s3FKnzncibe3hVx/mxkXBQpbS+OOblgDNOWQzfEjc=
    ```

## Optional GitHub Repository Variables

These defaults already match the current server:

- `DEPLOY_DIR`
  - Default: `/home/ubuntu/services/multica`
- `DEPLOY_APP_USER`
  - Default: `ubuntu`

## Server Expectations

- `louis.ning` can SSH into the server
- `louis.ning` has passwordless `sudo`
- `/home/ubuntu/services/multica` exists and is a git worktree
- Docker and Docker Compose are available on the server
- `.env` already exists at `/home/ubuntu/services/multica/.env`

## Current Server Mapping

- SSH login user: `louis.ning`
- Deploy path: `/home/ubuntu/services/multica`
- Worktree owner: `ubuntu`
- Git remote: `git@github.com:ninggc/multica.git`

## How to Run

1. Open `ninggc/multica` in GitHub.
2. Go to `Actions`.
3. Open `Deploy Self-Host`.
4. Click `Run workflow`.
5. Keep `ref=main` unless you intentionally want another ref.

## Rollback

Each deploy creates a timestamped backup directory on the server under:

- `/home/ubuntu/multica-release-backups/`

Every backup includes:

- `.env.backup`
- `postgres.dump`
- `postgres-globals.sql`
- `backend_uploads.tgz`
- `worktree-no-git.tgz`
- `predeploy-status.txt`
- `predeploy-working-tree.patch`
- `predeploy-diffstat.txt`
- `SHA256SUMS`

The backup logic lives in:

- `scripts/selfhost-backup.sh`

The workflow is designed for the current Ubuntu host and current directory layout. If the server path or SSH user changes later, update the repository variables or the workflow file before the next deployment.
