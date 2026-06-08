import { readVersionInfo } from "./version-info";

// Baked at build time by .deploy/deploy.sh (COMMIT_SHA, BUILD_TIME exports
// before `pnpm --filter @multica/web build`). Force-static so the response
// is captured at build time and survives launchd restarts without needing
// the runtime env to carry the SHA forward.
export const dynamic = "force-static";

export function GET() {
  const info = readVersionInfo({
    COMMIT_SHA: process.env.COMMIT_SHA,
    BUILD_TIME: process.env.BUILD_TIME,
  });
  return Response.json(info, {
    headers: {
      "cache-control": "no-store, must-revalidate",
      "x-commit-sha": info.commit,
    },
  });
}
