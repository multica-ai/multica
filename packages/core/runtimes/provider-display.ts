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
