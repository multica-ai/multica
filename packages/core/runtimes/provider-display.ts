const PROVIDER_DISPLAY_NAMES: Record<string, string> = {
  gemini: "AGY",
};

export function displayProviderName(provider: string | null | undefined): string {
  const normalized = provider?.trim();
  if (!normalized) return "Runtime";
  return (
    PROVIDER_DISPLAY_NAMES[normalized.toLowerCase()] ??
    `${normalized.slice(0, 1).toUpperCase()}${normalized.slice(1)}`
  );
}

export function displayRuntimeName(
  name: string | null | undefined,
  provider: string | null | undefined,
): string {
  const normalized = name?.trim();
  if (!normalized) return displayProviderName(provider);
  if (provider?.trim().toLowerCase() !== "gemini") return normalized;
  return normalized.replace(/^gemini(?=\s*(?:\(|$))/i, "AGY");
}
