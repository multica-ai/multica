/**
 * Utility functions for file tree operations
 */
import type { FileTreeNode } from '../../../shared/electron-api'

/**
 * Exclude hidden files (those starting with '.') from the list
 */
export const excludeHiddenFiles = (nodes: FileTreeNode[]): FileTreeNode[] =>
  nodes.filter((n) => !n.name.startsWith('.'))
