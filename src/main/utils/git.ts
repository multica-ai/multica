/**
 * Git utilities for detecting repository information
 */
import { execFile } from 'child_process'
import { existsSync } from 'fs'
import { join } from 'path'

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
