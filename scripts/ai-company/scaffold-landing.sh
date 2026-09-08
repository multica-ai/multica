#!/usr/bin/env bash
# Scaffold a minimal landing-page product repo from company templates.
set -euo pipefail

TARGET="${1:?usage: scaffold-landing.sh TARGET_DIR}"
MULTICA_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET="$(mkdir -p "$TARGET" && cd "$TARGET" && pwd)"
SLUG="landing-tool-a"
PKG="@landing-tool/web"

echo "Scaffolding landing product at $TARGET"

bash "$MULTICA_ROOT/.ai-company/harness/install.sh" "$TARGET"
cp -R "$MULTICA_ROOT/.ai-company/examples/$SLUG" "$TARGET/.delivery/$SLUG"

# Root package.json
cat >"$TARGET/package.json" <<'EOF'
{
  "name": "landing-tool-a",
  "private": true,
  "scripts": {
    "typecheck": "pnpm -r typecheck",
    "test": "vitest run"
  },
  "devDependencies": {
    "typescript": "^5.7.3",
    "vitest": "^3.0.5"
  },
  "packageManager": "pnpm@9.15.4"
}
EOF

cat >"$TARGET/pnpm-workspace.yaml" <<'EOF'
packages:
  - "apps/*"
EOF

cat >"$TARGET/tsconfig.json" <<'EOF'
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "skipLibCheck": true,
    "noEmit": true
  },
  "include": ["vitest.config.ts", "apps/**/*.ts", "apps/**/*.tsx"]
}
EOF

cat >"$TARGET/vitest.config.ts" <<'EOF'
import { defineConfig } from "vitest/config";
export default defineConfig({ test: { include: ["apps/**/*.test.ts"], environment: "node" } });
EOF

cat >"$TARGET/Makefile" <<'EOF'
.PHONY: check test
test:
	@echo "no go server"
check:
	pnpm typecheck
	pnpm test
EOF

mkdir -p "$TARGET/apps/web/app/privacy" "$TARGET/apps/web/app/terms"

cat >"$TARGET/apps/web/package.json" <<EOF
{
  "name": "$PKG",
  "private": true,
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "typecheck": "tsc -p tsconfig.json --noEmit"
  },
  "dependencies": {
    "next": "^15.1.6",
    "react": "^19.0.0",
    "react-dom": "^19.0.0"
  },
  "devDependencies": {
    "@types/node": "^22.10.7",
    "@types/react": "^19.0.7",
    "@types/react-dom": "^19.0.3",
    "typescript": "^5.7.3"
  }
}
EOF

cat >"$TARGET/apps/web/tsconfig.json" <<'EOF'
{
  "extends": "../../tsconfig.json",
  "compilerOptions": {
    "lib": ["dom", "dom.iterable", "esnext"],
    "jsx": "preserve",
    "allowJs": true,
    "incremental": true,
    "isolatedModules": true
  },
  "include": ["next-env.d.ts", "**/*.ts", "**/*.tsx"]
}
EOF

cat >"$TARGET/apps/web/next.config.ts" <<'EOF'
import type { NextConfig } from "next";
const nextConfig: NextConfig = { reactStrictMode: true };
export default nextConfig;
EOF

cat >"$TARGET/apps/web/next-env.d.ts" <<'EOF'
/// <reference types="next" />
/// <reference types="next/image-types/global" />
EOF

cat >"$TARGET/apps/web/app/layout.tsx" <<'EOF'
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "JSON Formatter — Fast & Private",
  description: "Format JSON in your browser. No upload.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
EOF

cat >"$TARGET/apps/web/app/page.tsx" <<'EOF'
export default function HomePage() {
  return (
    <main style={{ padding: "2rem", fontFamily: "system-ui" }}>
      <h1>JSON Formatter</h1>
      <p>Tool UI placeholder — implement in TICKET-003.</p>
    </main>
  );
}
EOF

cat >"$TARGET/apps/web/app/privacy/page.tsx" <<'EOF'
export default function PrivacyPage() {
  return <main style={{ padding: "2rem" }}><h1>Privacy</h1></main>;
}
EOF

cat >"$TARGET/apps/web/app/terms/page.tsx" <<'EOF'
export default function TermsPage() {
  return <main style={{ padding: "2rem" }}><h1>Terms</h1></main>;
}
EOF

cat >"$TARGET/apps/web/app/json.test.ts" <<'EOF'
// @vitest-environment node
import { describe, it } from "vitest";
describe("json tool", () => {
  it.todo("formats valid json (TICKET-003)");
});
EOF

cat >"$TARGET/.gitignore" <<'EOF'
node_modules
.next
.env*
.DS_Store
EOF

cat >"$TARGET/README.md" <<EOF
# landing-tool-a

Lightweight landing + tool page — AI company **second product line** example.

\`\`\`bash
pnpm install && make check
pnpm --filter $PKG dev
\`\`\`

Delivery: \`.delivery/$SLUG/\`
EOF

if [ ! -d "$TARGET/.git" ]; then
  git -C "$TARGET" init -q
fi

echo "Done. Next:"
echo "  cd $TARGET && pnpm install && make check"
echo "  bash $MULTICA_ROOT/scripts/ai-company/bootstrap-project.sh $TARGET --create-repo --push --sync-backlog"
