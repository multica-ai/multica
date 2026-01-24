export function getBaseName(inputPath: string, fallback = 'Root'): string {
  if (!inputPath) {
    return fallback
  }

  const trimmed = inputPath.replace(/[\\/]+$/, '')
  if (!trimmed) {
    return fallback
  }

  const parts = trimmed.split(/[\\/]/)
  return parts[parts.length - 1] || fallback
}
