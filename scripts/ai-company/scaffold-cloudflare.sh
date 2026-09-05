#!/usr/bin/env bash
# Scaffold a Cloudflare Pages (+ optional Workers) product repo from company templates.
set -euo pipefail

TARGET="${1:?usage: scaffold-cloudflare.sh TARGET_DIR [SLUG] [SITE_TITLE]}"
SLUG="${2:-cloudflare-site}"
SITE_TITLE="${3:-New Site}"
MULTICA_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET="$(mkdir -p "$TARGET" && cd "$TARGET" && pwd)"
PKG="@${SLUG}/web"
EXAMPLE="$MULTICA_ROOT/.ai-company/examples/cloudflare-site"

echo "Scaffolding Cloudflare site at $TARGET (slug=$SLUG)"

bash "$MULTICA_ROOT/.ai-company/harness/install.sh" "$TARGET"
# Cloudflare-only sites have no OpenAPI — drop optional API gate to avoid empty-workflow CI noise.
rm -f "$TARGET/.github/workflows/api-contract-gate.yml"
mkdir -p "$TARGET/.github/workflows"
if [ -f "$MULTICA_ROOT/.ai-company/harness/scaffold/.github/workflows/cloudflare-pages-check.yml" ]; then
  cp "$MULTICA_ROOT/.ai-company/harness/scaffold/.github/workflows/cloudflare-pages-check.yml" \
    "$TARGET/.github/workflows/cloudflare-pages-check.yml"
fi
mkdir -p "$TARGET/.delivery/$SLUG"
cp -R "$EXAMPLE/." "$TARGET/.delivery/$SLUG/"

# Patch brief title line if placeholder
if [ -f "$TARGET/.delivery/$SLUG/brief.md" ]; then
  sed -i '' "s/cloudflare-site/$SLUG/g" "$TARGET/.delivery/$SLUG/brief.md" 2>/dev/null || \
    sed -i "s/cloudflare-site/$SLUG/g" "$TARGET/.delivery/$SLUG/brief.md"
fi

cat >"$TARGET/package.json" <<EOF
{
  "name": "$SLUG",
  "private": true,
  "scripts": {
    "typecheck": "pnpm -r typecheck",
    "test": "vitest run",
    "build": "pnpm --filter $PKG build",
    "visual-check": "playwright test --grep @visual"
  },
  "devDependencies": {
    "@playwright/test": "^1.51.0",
    "typescript": "^5.7.3",
    "vitest": "^3.0.5",
    "wrangler": "^4.14.0"
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

cat >"$TARGET/wrangler.toml" <<EOF
# Cloudflare Pages — see https://developers.cloudflare.com/pages/
name = "$SLUG"
compatibility_date = "2024-11-01"
pages_build_output_dir = "apps/web/dist"
EOF

cat >"$TARGET/Makefile" <<'EOF'
.PHONY: check test build visual-check
test:
	@echo "no go server"
check:
	pnpm typecheck
	pnpm test
build:
	pnpm build
visual-check:
	pnpm exec playwright test --grep @visual
EOF

mkdir -p "$TARGET/apps/web/src" "$TARGET/apps/web/public"

cat >"$TARGET/apps/web/package.json" <<EOF
{
  "name": "$PKG",
  "private": true,
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview",
    "typecheck": "tsc -p tsconfig.json --noEmit"
  },
  "dependencies": {
    "react": "^19.0.0",
    "react-dom": "^19.0.0"
  },
  "devDependencies": {
    "@types/react": "^19.0.7",
    "@types/react-dom": "^19.0.3",
    "@vitejs/plugin-react": "^4.3.4",
    "typescript": "^5.7.3",
    "vite": "^6.0.7"
  }
}
EOF

cat >"$TARGET/apps/web/tsconfig.json" <<'EOF'
{
  "extends": "../../tsconfig.json",
  "compilerOptions": {
    "lib": ["dom", "dom.iterable", "esnext"],
    "jsx": "react-jsx",
    "allowJs": true,
    "isolatedModules": true
  },
  "include": ["src/**/*.ts", "src/**/*.tsx", "vite.config.ts"]
}
EOF

cat >"$TARGET/apps/web/vite.config.ts" <<'EOF'
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: { outDir: "dist", emptyOutDir: true },
});
EOF

cat >"$TARGET/apps/web/index.html" <<EOF
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>$SITE_TITLE</title>
    <meta name="description" content="$SITE_TITLE — fast, private, Cloudflare Pages." />
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
EOF

cat >"$TARGET/apps/web/src/main.tsx" <<'EOF'
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
EOF

cat >"$TARGET/apps/web/src/App.tsx" <<'EOF'
export function App() {
  return (
    <main
      data-testid="c-main"
      style={{ padding: "2rem", fontFamily: "system-ui, sans-serif", maxWidth: 720, margin: "0 auto" }}
    >
      <header data-testid="c-nav">
        <strong>Site brand</strong>
      </header>
      <h1 data-testid="c-hero">Site placeholder</h1>
      <p>Implement core UI in TICKET-003 per brief.md.</p>
      <button type="button" data-testid="i-cta">Primary CTA</button>
    </main>
  );
}
EOF

cat >"$TARGET/apps/web/src/app.test.ts" <<'EOF'
// @vitest-environment node
import { describe, it } from "vitest";
describe("app shell", () => {
  it.todo("renders core UI (TICKET-003)");
});
EOF

cat >"$TARGET/playwright.config.ts" <<EOF
import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: "list",
  use: {
    baseURL: "http://127.0.0.1:4173",
    trace: "on-first-retry",
  },
  expect: {
    toHaveScreenshot: { maxDiffPixelRatio: 0.02 },
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"], channel: process.env.PW_CHANNEL || "chrome" } }],
  webServer: {
    command: "pnpm --filter $PKG exec vite --host 127.0.0.1 --port 4173",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
EOF

mkdir -p "$TARGET/e2e"
cat >"$TARGET/e2e/visual.spec.ts" <<'EOF'
import { test, expect } from "@playwright/test";

test.describe("@visual replica gate", () => {
  test("home desktop", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByTestId("c-hero")).toBeVisible();
    await expect(page).toHaveScreenshot("home-desktop.png", {
      maxDiffPixelRatio: 0.02,
    });
  });

  test("home mobile 375", async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto("/");
    await expect(page.getByTestId("c-hero")).toBeVisible();
    await expect(page).toHaveScreenshot("home-mobile-375.png", {
      maxDiffPixelRatio: 0.02,
    });
  });
});
EOF

cat >"$TARGET/.gitignore" <<'EOF'
node_modules
dist
.wrangler
.env*
.DS_Store
test-results
playwright-report
blob-report
# Keep e2e/**/*-snapshots/ committed as visual baselines
EOF

cat >"$TARGET/README.md" <<EOF
# $SLUG

Cloudflare Pages site — AI company **Cloudflare product line**.

\`\`\`bash
pnpm install && make check
pnpm --filter $PKG dev
pnpm build   # apps/web/dist for wrangler pages
\`\`\`

Delivery truth: \`.delivery/$SLUG/\`
Stack: **Cloudflare Pages + Wrangler only** (no Vercel).
EOF

if [ ! -d "$TARGET/.git" ]; then
  git -C "$TARGET" init -q
fi

echo "Done. Next:"
echo "  cd $TARGET && pnpm install && pnpm exec playwright install chromium"
echo "  make check && make visual-check   # first run: pnpm exec playwright test --grep @visual --update-snapshots"
echo "  bash $MULTICA_ROOT/scripts/ai-company/bootstrap-project.sh $TARGET --create-repo --push --sync-backlog"
