#!/usr/bin/env bash
# Mirror the current GitLab `main` branch + the triggering tag to the
# Lilith GitHub mirror (`CopilotDemo/multica`). The mirror's own
# `.github/workflows/lilith-desktop-release.yml` fires on the tag push
# and runs the matrix build + Aliyun OSS publish.
#
# Required env, configured in GitLab → Project Settings → CI/CD →
# Variables:
#
#   GITHUB_MIRROR_DEPLOY_KEY
#     SSH private key whose matching public key is registered as a
#     write-enabled deploy key on CopilotDemo/multica. Both variable
#     types are supported:
#       - "Variable" (plain): the value is the key body (multiline).
#         GitLab cannot mask multiline values, so don't tick "Masked".
#         Do tick "Protected" so it's only available on protected tags.
#       - "File": the value is a filesystem path GitLab populates with
#         the key body. Safer (the body never lands in logs).
#     Generate one with:
#       ssh-keygen -t ed25519 -N "" -C "gitlab-mirror" -f gitlab_mirror_key
#     Paste `gitlab_mirror_key.pub` into the GitHub repo's
#     Settings → Deploy keys (tick "Allow write access").
#     Paste `gitlab_mirror_key` into this GitLab CI variable.
#
# Optional env (with safe defaults):
#
#   GITHUB_MIRROR_REMOTE   default `git@github.com:CopilotDemo/multica.git`
#   GITHUB_MIRROR_BRANCH   default `main`
#
# Provided automatically by GitLab on tag pipelines:
#
#   CI_COMMIT_TAG   the tag that triggered this job (e.g. `v0.2.35`
#                   or `0.2.35` — both shapes are accepted upstream).

set -euo pipefail

GITHUB_REMOTE="${GITHUB_MIRROR_REMOTE:-git@github.com:CopilotDemo/multica.git}"
GITHUB_BRANCH="${GITHUB_MIRROR_BRANCH:-main}"

if [ -z "${CI_COMMIT_TAG:-}" ]; then
  echo "❌ CI_COMMIT_TAG is empty — this job is only meaningful on tag pipelines."
  exit 1
fi

if [ -z "${GITHUB_MIRROR_DEPLOY_KEY:-}" ]; then
  echo "❌ GITHUB_MIRROR_DEPLOY_KEY is not set."
  echo "   Add it under GitLab Project Settings → CI/CD → Variables."
  echo "   See the comment block at the top of this script."
  exit 1
fi

# Stage the private key into a scoped location instead of touching
# ~/.ssh/id_* — the runner is shared between projects and clobbering
# the runner user's default identity would leak this credential to
# anything else that runs as gitlab-runner.
SSH_DIR="$HOME/.ssh"
KEY_PATH="$SSH_DIR/lilith_github_mirror"
KNOWN_HOSTS="$SSH_DIR/known_hosts_lilith_mirror"

mkdir -p "$SSH_DIR"
chmod 700 "$SSH_DIR"

# Support both variable shapes:
#   - "File" type: $GITHUB_MIRROR_DEPLOY_KEY is a path
#   - "Variable" type: $GITHUB_MIRROR_DEPLOY_KEY is the key body itself
if [ -f "$GITHUB_MIRROR_DEPLOY_KEY" ]; then
  cp "$GITHUB_MIRROR_DEPLOY_KEY" "$KEY_PATH"
else
  # printf preserves trailing newlines GitLab might trim; OpenSSH
  # requires a trailing newline at the end of the private key block.
  printf '%s\n' "$GITHUB_MIRROR_DEPLOY_KEY" > "$KEY_PATH"
fi
chmod 600 "$KEY_PATH"

# Pin GitHub's host keys to avoid the host-verification prompt and
# defeat any MITM that's somehow on the runner's outbound path. The
# `-t rsa,ecdsa,ed25519` covers whichever algorithm OpenSSH ends up
# negotiating.
ssh-keyscan -t rsa,ecdsa,ed25519 github.com >> "$KNOWN_HOSTS" 2>/dev/null
chmod 644 "$KNOWN_HOSTS"

# Constrain git to this identity + known_hosts for the mirror push.
# `IdentitiesOnly=yes` prevents OpenSSH from falling through to any
# agent-loaded keys or default ~/.ssh/id_* files — same key in,
# same key out, no surprises.
export GIT_SSH_COMMAND="ssh -i $KEY_PATH -o IdentitiesOnly=yes -o UserKnownHostsFile=$KNOWN_HOSTS -o StrictHostKeyChecking=yes"

# Attach the mirror as a one-shot remote. Re-runs in the same checkout
# would have left this in place; remove and re-add for a clean slate.
git remote remove github 2>/dev/null || true
git remote add github "$GITHUB_REMOTE"

# Fetch GitLab's current main HEAD explicitly. This matters when the
# tag points at a commit that ISN'T main HEAD (e.g. a hotfix tagged
# on a release branch, or a tag re-pushed to an older commit). We
# always want the mirror's main to be in sync with GitLab's main,
# never with whatever HEAD happens to be.
git fetch --no-tags origin "+refs/heads/main:refs/remotes/origin/main"

# Force-push main to the mirror first so the GitHub workflow files
# (.github/workflows/lilith-desktop-release.yml etc.) are present on
# the default branch BEFORE the tag push triggers them. Force is
# correct here: GitLab is the source of truth, the mirror must
# converge to it. If someone accidentally pushed directly to the
# GitHub repo, this corrects the divergence — that's the intent of
# a mirror.
echo "Pushing $GITHUB_BRANCH → $GITHUB_REMOTE …"
git push --force github "refs/remotes/origin/main:refs/heads/$GITHUB_BRANCH"

# Now push the triggering tag. This is what activates the GitHub
# Actions desktop-release workflow downstream.
echo "Pushing tag $CI_COMMIT_TAG → $GITHUB_REMOTE …"
git push github "refs/tags/$CI_COMMIT_TAG"

echo "✅ Mirrored to $GITHUB_REMOTE (branch=$GITHUB_BRANCH tag=$CI_COMMIT_TAG)"
