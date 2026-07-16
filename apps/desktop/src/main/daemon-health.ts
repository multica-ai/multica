import { readBoundedDaemonJSON } from "./daemon-json";

export interface PublicHealthPayload {
  status: "running" | "starting";
  os: string;
}

function isPublicHealthPayload(value: unknown): value is PublicHealthPayload {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const payload = value as Record<string, unknown>;
  const keys = Object.keys(payload);
  return (
    keys.length === 2 &&
    keys.includes("status") &&
    keys.includes("os") &&
    (payload.status === "running" || payload.status === "starting") &&
    typeof payload.os === "string"
  );
}

export async function fetchDaemonHealth(
  port: number,
): Promise<PublicHealthPayload | null> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 2_000);
  try {
    const response = await fetch(`http://127.0.0.1:${port}/health`, {
      redirect: "error",
      signal: controller.signal,
    });
    if (!response.ok) return null;
    const payload = await readBoundedDaemonJSON(response, controller.signal);
    return isPublicHealthPayload(payload) ? payload : null;
  } catch {
    return null;
  } finally {
    clearTimeout(timeout);
  }
}
