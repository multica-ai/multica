#!/usr/bin/env bash
# Tests for scripts/resolve-official-baseline.sh.
#
# Builds throwaway local git fixtures (a "checkout" repo and an "upstream"
# repo of canonical release tags) and asserts the official-baseline
# resolution semantics from the runtime-build-provenance plan (KTD1):
#   - nearest reachable tag via `git describe --abbrev=0` (no commit suffix)
#   - candidate verified against the canonical upstream tags
#   - explicit trusted override only when derivation is unavailable
#   - failure (never `dev`/hash/dirty) when neither source exists
#
# The upstream is a *local* fixture path so the resolver's `git ls-remote`
# verification can be exercised offline.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RESOLVER="$ROOT_DIR/scripts/resolve-official-baseline.sh"

pass=0
fails=0
RC_OUT=""
RC_STATUS=0

# Run the resolver inside a fixture checkout with a given upstream and
# (optional) trusted baseline. Captures stdout into RC_OUT and exit status
# into RC_STATUS.
run_resolver() {
	local checkout="$1" upstream="$2" trusted="${3:-}"
	local out rc=0
	out="$(
		cd "$checkout"
		export MULTICA_UPSTREAM_REMOTE="$upstream"
		export MULTICA_TRUSTED_BASELINE="$trusted"
		"$RESOLVER" 2>/dev/null
	)" || rc=$?
	RC_OUT="$out"
	RC_STATUS="$rc"
}

expect_ok() { # name expected-tag
	local name="$1" want="$2"
	if [ "$RC_STATUS" = "0" ] && [ "$RC_OUT" = "$want" ]; then
		echo "ok   - $name"
		pass=$((pass + 1))
	else
		echo "FAIL - $name: expected exit 0 out [$want], got exit $RC_STATUS out [$RC_OUT]"
		fails=$((fails + 1))
	fi
}

expect_fail() { # name
	local name="$1"
	if [ "$RC_STATUS" != "0" ]; then
		echo "ok   - $name"
		pass=$((pass + 1))
	else
		echo "FAIL - $name: expected non-zero exit, got exit 0 out [$RC_OUT]"
		fails=$((fails + 1))
	fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Create a fixture repo at $1 with one commit, then tag it with the remaining args.
make_repo() {
	local path="$1"
	shift
	mkdir -p "$path"
	git -C "$path" init -q
	git -C "$path" config user.email t@t.t
	git -C "$path" config user.name t
	git -C "$path" commit -q --allow-empty -m init
	for tag in "$@"; do git -C "$path" tag "$tag"; done
}

# Upstream canonical release tags.
make_repo "$WORK/upstream" v9.9.9 v1.2.3

# Checkout at exactly v9.9.9.
make_repo "$WORK/exact" v9.9.9

# Checkout with commits after v9.9.9.
make_repo "$WORK/posttag" v9.9.9
git -C "$WORK/posttag" commit -q --allow-empty -m c1
git -C "$WORK/posttag" commit -q --allow-empty -m c2

# Checkout at v9.9.9 with a dirty working tree.
make_repo "$WORK/dirty" v9.9.9
echo x >"$WORK/dirty/file"
git -C "$WORK/dirty" add file

# Checkout whose only tag is fork-only (not on the upstream).
make_repo "$WORK/fork" v7.7.7-forkonly

# Checkout with no tags at all.
make_repo "$WORK/notags"

# 1. exact official checkout, upstream carries the tag
run_resolver "$WORK/exact" "$WORK/upstream" ""
expect_ok "exact checkout resolves nearest upstream tag" "v9.9.9"

# 2. commits after the tag resolve to exactly the tag (no -N-g<hash> suffix)
run_resolver "$WORK/posttag" "$WORK/upstream" ""
expect_ok "post-tag commits resolve to exactly the tag" "v9.9.9"

# 3. dirty working tree does not add a dirty suffix
run_resolver "$WORK/dirty" "$WORK/upstream" ""
expect_ok "dirty working tree keeps the clean tag" "v9.9.9"

# 4. fork-only tag not on upstream, no override -> fail
run_resolver "$WORK/fork" "$WORK/upstream" ""
expect_fail "fork-only tag without override fails"

# 5. unreachable upstream, no override -> fail
run_resolver "$WORK/exact" "$WORK/does-not-exist" ""
expect_fail "unreachable upstream without override fails"

# 6. unreachable upstream + trusted override -> override
run_resolver "$WORK/exact" "$WORK/does-not-exist" "v8.8.8"
expect_ok "trusted override used when derivation unavailable" "v8.8.8"

# 7. unreachable upstream + invalid trusted baseline -> fail
run_resolver "$WORK/exact" "$WORK/does-not-exist" "garbage"
expect_fail "invalid trusted baseline fails"

# 8. checkout without tags, no override -> fail
run_resolver "$WORK/notags" "$WORK/upstream" ""
expect_fail "tagless checkout without override fails"

# 9. fork-only tag + trusted override -> override (derivation unavailable)
run_resolver "$WORK/fork" "$WORK/upstream" "v8.8.8"
expect_ok "trusted override used for fork-only tag" "v8.8.8"

echo
echo "pass=$pass fail=$fails"
[ "$fails" = "0" ] || exit 1
