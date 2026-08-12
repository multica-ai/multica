export interface ManagedRuntimeInstallResult {
  provider: string;
  version?: string;
  path: string;
  source: "user" | "managed";
  installed: boolean;
}

export function parseManagedRuntimeInstallResult(
  raw: string,
  expectedProvider: string,
): ManagedRuntimeInstallResult {
  const parsed: unknown = JSON.parse(raw);
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("managed runtime installer returned invalid JSON");
  }
  const value = parsed as Record<string, unknown>;
  if (
    value.provider !== expectedProvider ||
    typeof value.path !== "string" ||
    value.path.trim() === "" ||
    (value.source !== "user" && value.source !== "managed") ||
    typeof value.installed !== "boolean" ||
    (value.version !== undefined && typeof value.version !== "string")
  ) {
    throw new Error("managed runtime installer returned an invalid result");
  }
  return {
    provider: expectedProvider,
    version: value.version as string | undefined,
    path: value.path,
    source: value.source,
    installed: value.installed,
  };
}
