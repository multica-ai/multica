export interface SecureFillInput {
  serverUrl: string;
  agentToken: string;
  desktopToken: string;
  host: string;
  ref: string;
  vault: string;
  key: string;
  fetchImpl?: typeof fetch;
  fill: (ref: string, value: string) => Promise<void>;
  audit: (detail: Record<string, unknown>) => Promise<void>;
}

export interface SecureFillResult {
  ok: true;
  audit: { host: string; vault: string; key: string; ref: string };
}

/** Fetch a credential inside the trusted desktop process and inject it into
 * Chromium. The plaintext is deliberately confined to this stack frame. */
export async function performSecureFill(input: SecureFillInput): Promise<SecureFillResult> {
  const request = input.fetchImpl ?? fetch;
  const res = await request(
    `${input.serverUrl.replace(/\/+$/, "")}/api/cerebro/personal-browser/secure-fill`,
    {
      method: "POST",
      headers: {
        "content-type": "application/json",
        authorization: `Bearer ${input.desktopToken}`,
      },
      body: JSON.stringify({
        host: input.host,
        action: "secure-fill",
        vault: input.vault,
        key: input.key,
        agentToken: input.agentToken,
      }),
    },
  );
  if (!res.ok) throw new Error(`secure fill unavailable (status ${res.status})`);
  const payload = (await res.json()) as { value?: unknown };
  if (typeof payload.value !== "string") throw new Error("secure fill returned no value");
  await input.fill(input.ref, payload.value);
  const audit = { host: input.host, vault: input.vault, key: input.key, ref: input.ref };
  await input.audit(audit);
  return { ok: true, audit };
}
