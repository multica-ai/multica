export const DEFAULT_MAX_FILE_SIZE = 100 * 1024 * 1024;

let maxFileSize = DEFAULT_MAX_FILE_SIZE;

export function setMaxFileSize(bytes: number | undefined): void {
  maxFileSize =
    typeof bytes === "number" && Number.isSafeInteger(bytes) && bytes > 0
      ? bytes
      : DEFAULT_MAX_FILE_SIZE;
}

export function getMaxFileSize(): number {
  return maxFileSize;
}

export function formatFileSizeLimit(bytes: number): string {
  const gib = 1024 * 1024 * 1024;
  const mib = 1024 * 1024;
  if (bytes % gib === 0) return `${bytes / gib} GiB`;
  if (bytes % mib === 0) return `${bytes / mib} MiB`;
  return `${bytes.toLocaleString()} bytes`;
}
