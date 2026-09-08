#!/usr/bin/env bash
# Scaffold SaaS MVP repo: marketing + dashboard shell; payment paths human-only.
set -euo pipefail

TARGET="${1:?usage: scaffold-saas.sh TARGET_DIR}"
MULTICA_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET="$(mkdir -p "$TARGET" && cd "$TARGET" && pwd)"
SLUG="saas-stripe-mvp"

bash "$MULTICA_ROOT/.ai-company/harness/install.sh" "$TARGET"
cp -R "$MULTICA_ROOT/.ai-company/examples/$SLUG" "$TARGET/.delivery/$SLUG"

# Stricter merge policy for SaaS
cat >"$TARGET/.delivery/config/merge-policy.json" <<'EOF'
{
  "autoMergeEnabled": true,
  "requireLabels": ["agent-safe"],
  "branchNamePrefix": "cursor/",
  "allow": [
    "docs/**",
    "**/*.md",
    "**/*.test.ts",
    "**/*.test.tsx",
    "**/*_test.go",
    "apps/web/app/**",
    "packages/**"
  ],
  "deny": [
    "**/migrations/**",
    "**/payment/**",
    "**/billing/**",
    "**/auth/**",
    "**/stripe/**",
    "**/.env*",
    ".github/workflows/**"
  ]
}
EOF

# Reuse music-game structure pattern (minimal)
bash -c "
  SOURCE='$MULTICA_ROOT/../music-game-sea'
  if [ -d \"\$SOURCE/packages/core\" ]; then
    cp -R \"\$SOURCE/package.json\" \"\$SOURCE/pnpm-workspace.yaml\" \"\$SOURCE/tsconfig.json\" \"\$SOURCE/vitest.config.ts\" \"\$SOURCE/Makefile\" \"\$SOURCE/.gitignore\" '$TARGET/' 2>/dev/null || true
    mkdir -p '$TARGET/packages/core/src' '$TARGET/apps/web/app/dashboard' '$TARGET/server/cmd/api'
    cp -R \"\$SOURCE/packages/core/\"* '$TARGET/packages/core/' 2>/dev/null || true
    cp \"\$SOURCE/server/go.mod\" '$TARGET/server/' 2>/dev/null || true
    cp \"\$SOURCE/server/cmd/api/\"* '$TARGET/server/cmd/api/' 2>/dev/null || true
  fi
" || true

# Fallback minimal files if music-game-sea not present
if [ ! -f "$TARGET/package.json" ]; then
  cat >"$TARGET/package.json" <<'EOF'
{"name":"saas-stripe-mvp","private":true,"scripts":{"typecheck":"pnpm -r typecheck","test":"vitest run"},"devDependencies":{"typescript":"^5.7.3","vitest":"^3.0.5"},"packageManager":"pnpm@9.15.4"}
EOF
  echo 'packages:\n  - "apps/*"\n  - "packages/*"' >"$TARGET/pnpm-workspace.yaml"
  cat >"$TARGET/Makefile" <<'EOF'
test:
	cd server && go test ./...
check: test
	pnpm typecheck
	pnpm test
EOF
fi

mkdir -p "$TARGET/apps/web/app/dashboard"
cat >"$TARGET/apps/web/app/dashboard/page.tsx" <<'EOF'
export default function DashboardPage() {
  return (
    <main style={{ padding: "2rem" }}>
      <h1>Dashboard</h1>
      <p>Shell only — billing is human-only (PAY-001).</p>
    </main>
  );
}
EOF

cat >"$TARGET/README.md" <<EOF
# saas-stripe-mvp

SaaS landing + dashboard shell. **Stripe/auth = human-only.**

\`\`\`bash
pnpm install && make check
\`\`\`

See \`.delivery/$SLUG/human-only-queue.md\`
EOF

[ ! -d "$TARGET/.git" ] && git -C "$TARGET" init -q

echo "Scaffolded $TARGET"
echo "  deny paths: payment, billing, auth, stripe"
echo "  next: bootstrap-project.sh --create-repo --push --sync-backlog"
