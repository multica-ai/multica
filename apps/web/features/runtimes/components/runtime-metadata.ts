export function getMetadataString(
  metadata: Record<string, unknown>,
  key: string,
): string | null {
  const value = metadata?.[key];
  return typeof value === "string" && value ? value : null;
}

export function getRuntimeCliVersion(
  metadata: Record<string, unknown>,
): string | null {
  return getMetadataString(metadata, "version");
}

export function getMulticaCliVersion(
  metadata: Record<string, unknown>,
): string | null {
  return getMetadataString(metadata, "cli_version");
}
