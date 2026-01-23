import * as path from 'path'

export function isValidPath(inputPath: string): boolean {
  if (!path.isAbsolute(inputPath)) {
    return false
  }

  if (/(^|[\\/])\.\.($|[\\/])/.test(inputPath)) {
    return false
  }

  return true
}
