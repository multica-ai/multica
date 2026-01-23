import * as path from 'path'

// Regex to match Windows absolute paths (e.g., C:\Users or D:\)
const WINDOWS_ABSOLUTE_PATH_REGEX = /^[a-zA-Z]:[/\\]/

export function isValidPath(inputPath: string): boolean {
  // Check for absolute path (POSIX or Windows)
  const isAbsolute = path.isAbsolute(inputPath) || WINDOWS_ABSOLUTE_PATH_REGEX.test(inputPath)
  if (!isAbsolute) {
    return false
  }

  if (/(^|[\\/])\.\.($|[\\/])/.test(inputPath)) {
    return false
  }

  return true
}
