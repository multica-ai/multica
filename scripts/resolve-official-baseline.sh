#!/usr/bin/env bash
# Resolve the official release baseline tag for a self-hosted build.
#
# Authority order (runtime-build-provenance plan, KTD1):
#   1. Derive the nearest reachable official tag with
#      `git describe --tags --abbrev=0 --match 'v[0-9]*'`. The result is
#      exactly the tag, never a commit-distance/hash suffix.
#   2. Verify the candidate exists among the canonical upstream release
#      tags (`git ls-remote --tags` against MULTICA_UPSTREAM_REMOTE). A tag
#      that is only local/fork-created, or an upstream that cannot be
#      reached, leaves derivation unavailable.
#   3. Fall back to an explicit trusted operator baseline
#      (MULTICA_TRUSTED_BASELINE) only when derivation is unavailable.
#   4. Exit non-zero when neither is available. Never emit `dev`, a commit
#      hash, or a dirty suffix as a valid baseline.
#
# Output: exactly the baseline tag on stdout (nothing else); diagnostics on
# stderr. Operates on the current git repository, so run it from the
# checkout root on the host before Docker context creation (`.dockerignore`
# excludes `.git`, so this cannot run inside Docker). See
# scripts/resolve-official-baseline.test.sh for the behavior contract.
set -euo pipefail

# Canonical upstream whose tags define an "official" release. Kept as a
# single constant so a heavily-forked deployment that tracks a different
# upstream can retarget verification in one place.
UPSTREAM_REMOTE="${MULTICA_UPSTREAM_REMOTE:-https://github.com/multica-ai/multica}"
TRUSTED_BASELINE="${MULTICA_TRUSTED_BASELINE:-}"

fail() {
	echo "resolve-official-baseline: $*" >&2
	echo "  Derivation unavailable. Set MULTICA_TRUSTED_BASELINE=vX.Y.Z to supply an explicit trusted baseline." >&2
	exit 1
}

# A valid official tag starts with 'v' + digit and carries no git-describe
# commit-distance/hash suffix (-<n>-g<hash>).
is_official_tag() {
	local tag="$1"
	[[ "$tag" =~ ^v[0-9] ]] || return 1
	# Defensive: abbrev=0 already prevents describe suffixes.
	[[ "$tag" =~ -[0-9]+-g[0-9a-f]{4,}$ ]] && return 1
	return 0
}

# Return 0 if $1 is among the upstream's tags, 1 if absent, 2 if the
# upstream cannot be reached.
verify_upstream() {
	local candidate="$1" refs names
	if ! refs=$(git ls-remote --tags "$UPSTREAM_REMOTE" 2>/dev/null); then
		return 2
	fi
	# ls-remote --tags emits: <sha>\trefs/tags/<tag> and, for annotated tags,
	# <sha>\trefs/tags/<tag>^{}. Strip down to one clean tag name per line.
	names=$(printf '%s\n' "$refs" | sed 's#.*refs/tags/##; s#\^{}$##')
	if printf '%s\n' "$names" | grep -Fxq "$candidate"; then
		return 0
	fi
	return 1
}

candidate=""
reason=""

if candidate=$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null) && [ -n "$candidate" ]; then
	if verify_upstream "$candidate"; then
		printf '%s\n' "$candidate"
		exit 0
	elif [ "$?" = "2" ]; then
		reason="could not reach upstream '$UPSTREAM_REMOTE' to verify nearest tag '$candidate'"
	else
		reason="nearest tag '$candidate' is not present on upstream '$UPSTREAM_REMOTE'"
	fi
else
	reason="no reachable v* tag in this checkout"
fi

# Derivation unavailable — fall back to the explicit trusted baseline.
if [ -n "$TRUSTED_BASELINE" ]; then
	if is_official_tag "$TRUSTED_BASELINE"; then
		printf '%s\n' "$TRUSTED_BASELINE"
		exit 0
	fi
	fail "MULTICA_TRUSTED_BASELINE='$TRUSTED_BASELINE' is not a valid official tag (expected a v... tag). $reason"
fi

fail "cannot establish official baseline: $reason"
