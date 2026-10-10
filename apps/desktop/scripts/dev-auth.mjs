import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

/**
 * Expose the already-provisioned local development profile token to Vite.
 * The renderer accepts it only for a loopback API and only in a dev build.
 */
export function applyDevAuthEnv(env) {
  if (env.VITE_DESKTOP_DEV_AUTH_TOKEN) return true;

  const profile = env.MULTICA_DEV_PROFILE?.trim();
  if (!profile || !/^[a-zA-Z0-9._-]+$/.test(profile)) return false;

  const profilesHome =
    env.MULTICA_DEV_PROFILES_HOME?.trim() || join(homedir(), ".multica", "profiles");

  try {
    const raw = readFileSync(join(profilesHome, profile, "config.json"), "utf8");
    const parsed = JSON.parse(raw);
    const token = typeof parsed.token === "string" ? parsed.token.trim() : "";
    if (!token) return false;
    env.VITE_DESKTOP_DEV_AUTH_TOKEN = token;
    return true;
  } catch {
    return false;
  }
}
