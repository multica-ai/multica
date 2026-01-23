/**
 * Git utilities for detecting repository information
 */
import { execFile } from 'child_process'
import { existsSync, readFileSync, statSync } from 'fs'
import { isAbsolute, join, resolve } from 'path'

/**
 * Get the current git branch name for a directory
 * Returns undefined if the directory is not a git repository or git is not available
 */
export function getGitBranch(directory: string): Promise<string | undefined> {
  return new Promise((resolve) => {
    // Quick check: does .git exist?
    if (!existsSync(join(directory, '.git'))) {
      resolve(undefined)
      return
    }

    execFile(
      'git',
      ['rev-parse', '--abbrev-ref', 'HEAD'],
      { cwd: directory, timeout: 3000 },
      (error, stdout) => {
        if (error) {
          resolve(undefined)
          return
        }
        const branch = stdout.trim()
        resolve(branch || undefined)
      }
    )
  })
}

/**
 * Resolve the git HEAD path for a working directory.
 * Supports both regular repos and worktrees (where .git is a file).
 */
export function getGitHeadPath(directory: string): string | undefined {
  const gitEntry = join(directory, '.git')
  if (!existsSync(gitEntry)) {
    return undefined
  }

  try {
    const stats = statSync(gitEntry)
    if (stats.isDirectory()) {
      const headPath = join(gitEntry, 'HEAD')
      return existsSync(headPath) ? headPath : undefined
    }

    if (stats.isFile()) {
      const raw = readFileSync(gitEntry, 'utf8')
      const match = raw.match(/gitdir:\s*(.+)/i)
      if (!match) return undefined
      const gitDirRaw = match[1].trim()
      const gitDir = isAbsolute(gitDirRaw) ? gitDirRaw : resolve(directory, gitDirRaw)
      const headPath = join(gitDir, 'HEAD')
      return existsSync(headPath) ? headPath : undefined
    }
  } catch {
    return undefined
  }

  return undefined
}
