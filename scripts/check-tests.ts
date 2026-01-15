#!/usr/bin/env tsx
/**
 * Pre-commit hook script that checks if staged source files have corresponding test files.
 * This is an advisory check - it warns but does not block commits.
 */

import { execSync } from 'child_process'
import * as fs from 'fs'
import * as path from 'path'

// ANSI color codes
const colors = {
  yellow: '\x1b[33m',
  green: '\x1b[32m',
  red: '\x1b[31m',
  reset: '\x1b[0m',
  bold: '\x1b[1m'
}

// Source directories that require tests
const SOURCE_DIRS = ['src/main', 'src/shared']

// Mapping from source paths to test paths
function getExpectedTestPath(sourcePath: string): string | null {
  // src/main/conductor/Conductor.ts -> tests/unit/main/conductor/Conductor.test.ts
  // src/shared/types.ts -> tests/unit/shared/types.test.ts

  for (const sourceDir of SOURCE_DIRS) {
    if (sourcePath.startsWith(sourceDir)) {
      const relativePath = sourcePath.slice(sourceDir.length + 1) // +1 for the slash
      const dirName = sourceDir.split('/')[1] // 'main' or 'shared'
      const testPath = `tests/unit/${dirName}/${relativePath.replace(/\.ts$/, '.test.ts')}`
      return testPath
    }
  }
  return null
}

// Get list of staged files
function getStagedFiles(): string[] {
  try {
    const output = execSync('git diff --cached --name-only --diff-filter=ACM', {
      encoding: 'utf-8'
    })
    return output.trim().split('\n').filter(Boolean)
  } catch {
    return []
  }
}

// Check if a file is a TypeScript source file (not a test, not a type definition)
function isSourceFile(filePath: string): boolean {
  if (!filePath.endsWith('.ts') && !filePath.endsWith('.tsx')) {
    return false
  }
  if (filePath.includes('.test.') || filePath.includes('.spec.')) {
    return false
  }
  if (filePath.endsWith('.d.ts')) {
    return false
  }
  return SOURCE_DIRS.some((dir) => filePath.startsWith(dir))
}

// Check if a file is a test file
function isTestFile(filePath: string): boolean {
  return filePath.includes('.test.') || filePath.includes('.spec.')
}

// Main function
function main(): void {
  const stagedFiles = getStagedFiles()

  if (stagedFiles.length === 0) {
    console.log(`${colors.green}No staged files to check.${colors.reset}`)
    return
  }

  const sourceFiles = stagedFiles.filter(isSourceFile)
  const testFiles = stagedFiles.filter(isTestFile)

  if (sourceFiles.length === 0) {
    console.log(`${colors.green}No source files modified.${colors.reset}`)
    return
  }

  // Check which source files are missing corresponding test files
  const missingTests: { source: string; expectedTest: string }[] = []

  for (const sourceFile of sourceFiles) {
    const expectedTestPath = getExpectedTestPath(sourceFile)
    if (!expectedTestPath) continue

    // Check if the test file is staged or already exists
    const testFileStaged = testFiles.some(
      (tf) =>
        tf === expectedTestPath ||
        tf.includes(path.basename(sourceFile).replace(/\.tsx?$/, '.test.'))
    )

    const testFileExists = fs.existsSync(expectedTestPath)

    if (!testFileStaged && !testFileExists) {
      missingTests.push({ source: sourceFile, expectedTest: expectedTestPath })
    }
  }

  if (missingTests.length > 0) {
    console.log(
      `\n${colors.yellow}${colors.bold}Warning: Source files modified without corresponding tests${colors.reset}\n`
    )
    console.log('  Modified source files:')
    for (const { source } of missingTests) {
      console.log(`    ${colors.yellow}- ${source}${colors.reset}`)
    }
    console.log('\n  Expected test files (not found):')
    for (const { expectedTest } of missingTests) {
      console.log(`    ${colors.yellow}- ${expectedTest}${colors.reset}`)
    }
    console.log(`\n  ${colors.bold}Consider adding tests for your changes.${colors.reset}`)
    console.log(`  Run 'pnpm test:run' to verify existing tests still pass.\n`)
  } else {
    console.log(`${colors.green}All modified source files have corresponding tests.${colors.reset}`)
  }
}

main()
