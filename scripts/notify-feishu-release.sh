#!/usr/bin/env bash
#
# notify-feishu-release.sh — post a Feishu interactive card when a release
# tag lands on main. Designed to run from .gitlab-ci.yml on tag pipelines,
# but works locally too (set the env vars below by hand).
#
# Required env:
#   CI_COMMIT_TAG         release tag, e.g. "0.0.2"
#   CI_COMMIT_SHA         commit the tag points at
#   CI_PROJECT_URL        e.g. https://gitlab.lilithgame.com/devops/multica
#   FEISHU_WEBHOOK_URL    https://open.feishu.cn/open-apis/bot/v2/hook/<id>
#
# Optional:
#   --dry-run             print rendered payload, do not POST
#
# Skip behavior (exits 0, no notification):
#   - tag is not reachable from origin/main
#   - tag is a lightweight tag (no annotation)
#   - tag annotation is empty after stripping the signature
#
# The first two skips are silent guards so unrelated tag pushes (upstream
# sync tags, hotfix branches, accidental lightweight tags) don't broadcast
# noise. Ship the release with `git tag -a <ver> -m "..."` to trigger.

set -euo pipefail

DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
    DRY_RUN=true
fi

TAG="${CI_COMMIT_TAG:?CI_COMMIT_TAG is required (this script only runs on tag pipelines)}"
COMMIT_SHA="${CI_COMMIT_SHA:?CI_COMMIT_SHA is required}"
PROJECT_URL="${CI_PROJECT_URL:-https://gitlab.lilithgame.com/devops/multica}"

if ! $DRY_RUN; then
    : "${FEISHU_WEBHOOK_URL:?FEISHU_WEBHOOK_URL is required}"
fi

# 1. Tag must be reachable from <remote>/main. In GitLab CI the remote is
#    "origin" by default; locally for testing we may need RELEASE_REMOTE=gitlab
#    because "origin" can point at the upstream OSS repo on the developer's
#    machine. The extra reachability check keeps an annotated tag accidentally
#    pushed on a side branch from broadcasting.
RELEASE_REMOTE="${RELEASE_REMOTE:-origin}"
if ! git rev-parse --quiet --verify "$RELEASE_REMOTE/main" >/dev/null; then
    git fetch --no-tags --depth=200 "$RELEASE_REMOTE" main >/dev/null 2>&1 || true
fi
if ! git merge-base --is-ancestor "$COMMIT_SHA" "$RELEASE_REMOTE/main" 2>/dev/null; then
    echo "Tag $TAG is not reachable from $RELEASE_REMOTE/main — skipping notification."
    exit 0
fi

# 2. Tag must be annotated. Lightweight tags have no message of their own,
#    so we treat them as "not a release" and bail out silently.
TAG_TYPE="$(git cat-file -t "$TAG")"
if [[ "$TAG_TYPE" != "tag" ]]; then
    echo "Tag $TAG is a lightweight tag — skipping. Use 'git tag -a $TAG -m \"...\"' to trigger a release notification."
    exit 0
fi

# 3. Extract the annotation body. `%(contents)` includes any GPG/SSH signature
#    block; we strip everything from the BEGIN line down so the card carries
#    only what the releaser wrote.
ANNOTATION="$(git tag -l --format='%(contents)' "$TAG" \
    | sed -E '/^-----BEGIN (PGP|SSH) SIGNATURE-----/,$d')"
ANNOTATION="${ANNOTATION#"${ANNOTATION%%[![:space:]]*}"}"  # ltrim
ANNOTATION="${ANNOTATION%"${ANNOTATION##*[![:space:]]}"}"  # rtrim

if [[ -z "$ANNOTATION" ]]; then
    echo "Tag $TAG has an empty annotation — skipping."
    exit 0
fi

# 4. Split annotation into subject (line 1, becomes card title) and body
#    (everything after the first blank line, rendered as the card's main
#    markdown block). This mirrors the git-commit convention so the
#    releaser's `git tag -a` message acts as a self-formatting card.
ANNOTATION_SUBJECT="$(printf '%s\n' "$ANNOTATION" | sed -n '1p')"
ANNOTATION_BODY="$(printf '%s\n' "$ANNOTATION" \
    | awk 'NR==1{next} body || NF{body=1; print}')"
# Trim trailing whitespace.
ANNOTATION_BODY="${ANNOTATION_BODY%"${ANNOTATION_BODY##*[![:space:]]}"}"

# 5. Render the card. jq's --arg keeps newlines and special chars safe.
SHORT_SHA="${COMMIT_SHA:0:7}"
TAG_URL="$PROJECT_URL/-/tags/$TAG"
COMMIT_URL="$PROJECT_URL/-/commit/$COMMIT_SHA"
SHIP_URL="https://ship.lilithgames.com"

PAYLOAD="$(jq -n \
    --arg tag "$TAG" \
    --arg subject "$ANNOTATION_SUBJECT" \
    --arg body "$ANNOTATION_BODY" \
    --arg short_sha "$SHORT_SHA" \
    --arg tag_url "$TAG_URL" \
    --arg commit_url "$COMMIT_URL" \
    --arg ship_url "$SHIP_URL" \
'{
    msg_type: "interactive",
    card: {
        config: {wide_screen_mode: true},
        header: {
            title: {tag: "plain_text", content: $subject},
            subtitle: {tag: "plain_text", content: ("已上线 Ship  ·  Tag " + $tag)},
            template: "blue"
        },
        elements: (
            (if $body == "" then [] else [{tag: "markdown", content: $body}, {tag: "hr"}] end)
            + [
                {
                    tag: "action",
                    actions: [
                        {tag: "button", text: {tag: "plain_text", content: "🚀 打开 Ship"}, type: "primary", url: $ship_url}
                    ]
                },
                {
                    tag: "note",
                    elements: [
                        {tag: "lark_md", content: ("🔗 [Tag `" + $tag + "`](" + $tag_url + ")  ·  [commit `" + $short_sha + "`](" + $commit_url + ")")}
                    ]
                }
            ]
        )
    }
}')"

if $DRY_RUN; then
    echo "$PAYLOAD" | jq .
    exit 0
fi

# 6. POST and verify Feishu's success envelope.
echo "Sending release notification for $TAG..."
RESPONSE="$(curl -sS -X POST "$FEISHU_WEBHOOK_URL" \
    -H 'Content-Type: application/json' \
    -d "$PAYLOAD")"
echo "$RESPONSE"

if ! echo "$RESPONSE" | jq -e '(.code == 0) or (.StatusCode == 0)' >/dev/null; then
    echo "❌ Feishu webhook returned a non-success response."
    exit 1
fi

echo "✅ Notification sent for $TAG."
