export type DesktopBootstrapResult =
  | { kind: "success"; token: string }
  | { kind: "unsupported"; status: number }
  | { kind: "error"; status?: number; message: string };

const UNSUPPORTED_BOOTSTRAP_STATUSES = new Set([401, 403, 404, 405, 409]);

function normalizeApiBaseUrl(apiBaseUrl: string): string {
  return apiBaseUrl.endsWith("/") ? apiBaseUrl : `${apiBaseUrl}/`;
}

function readBootstrapToken(payload: unknown): string | null {
  if (!payload || typeof payload !== "object") return null;
  const token =
    "token" in payload && typeof payload.token === "string"
      ? payload.token
      : "access_token" in payload && typeof payload.access_token === "string"
        ? payload.access_token
        : null;
  return token && token.length > 0 ? token : null;
}

export async function requestDesktopBootstrapToken(
  apiBaseUrl: string,
): Promise<DesktopBootstrapResult> {
  const endpoint = new URL(
    "auth/bootstrap/token",
    normalizeApiBaseUrl(apiBaseUrl),
  ).toString();

  let response: Response;
  try {
    response = await fetch(endpoint, {
      method: "POST",
      headers: {
        Accept: "application/json",
      },
    });
  } catch (error) {
    return {
      kind: "error",
      message:
        error instanceof Error ? error.message : "Desktop bootstrap failed",
    };
  }

  if (UNSUPPORTED_BOOTSTRAP_STATUSES.has(response.status)) {
    return { kind: "unsupported", status: response.status };
  }

  if (!response.ok) {
    const message = await response.text().catch(() => "");
    return {
      kind: "error",
      status: response.status,
      message: message || response.statusText || "Desktop bootstrap failed",
    };
  }

  const payload = await response.json().catch(() => null);
  const token = readBootstrapToken(payload);
  if (!token) {
    return {
      kind: "error",
      status: response.status,
      message: "Desktop bootstrap response did not include a token",
    };
  }

  return { kind: "success", token };
}
