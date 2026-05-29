type RuntimeEnv = Record<string, string | undefined>;

export function resolveRemoteApiUrl(env: RuntimeEnv): string {
  const explicitRemote = env.REMOTE_API_URL?.trim();
  if (explicitRemote) return explicitRemote;

  const publicApi = env.NEXT_PUBLIC_API_URL?.trim();
  if (publicApi) return publicApi;

  // NOTE: Do NOT fall back to PORT here. In Next.js dev mode the server
  // sets PORT to its own port (3000) before evaluating next.config.ts, and
  // dotenv won't overwrite an existing env var — so PORT=3000 would make
  // rewrites point at the frontend itself instead of the backend.
  const port =
    env.BACKEND_PORT?.trim() ||
    env.API_PORT?.trim() ||
    env.SERVER_PORT?.trim();
  if (port) return `http://localhost:${port}`;

  return "http://localhost:8080";
}
