/**
 * Mode Semantic Configuration
 *
 * Normalizes different ACP agent modes (Claude Code, Codex, OpenCode) to
 * a unified UI representation with filtering and semantic categorization.
 *
 * Unknown modes are HIDDEN by default - must be explicitly added to config.
 */

import type { SessionMode } from '@agentclientprotocol/sdk'

/**
 * Semantic type for UI (icon + color):
 * - 'plan': Green, PenLine - planning mode
 * - 'readonly': Green, Shield - read-only/safe mode
 * - 'default': Gray, Circle - normal mode
 * - 'auto': Purple, Zap - high automation mode
 */
export type SemanticType = 'plan' | 'readonly' | 'default' | 'auto'

/**
 * Mode configuration entry
 */
interface ModeConfig {
  /** Whether to show this mode in the UI */
  visible: boolean
  /** Semantic type for UI rendering */
  semantic: SemanticType
}

/**
 * Mode configuration table
 *
 * Only modes listed here with visible: true are shown in UI.
 * Unknown modes are hidden by default.
 */
const MODE_CONFIG: Record<string, ModeConfig> = {
  // Claude Code (show 3 of 5)
  default: { visible: true, semantic: 'default' },
  acceptEdits: { visible: true, semantic: 'auto' }, // Auto-Accept
  plan: { visible: true, semantic: 'plan' },
  dontAsk: { visible: false, semantic: 'default' }, // internal
  bypassPermissions: { visible: false, semantic: 'auto' }, // dangerous

  // Codex (show 2 of 3)
  'read-only': { visible: true, semantic: 'readonly' }, // Suggest mode
  auto: { visible: true, semantic: 'auto' }, // Auto-Edit
  'full-access': { visible: false, semantic: 'auto' }, // Full-Auto, dangerous

  // OpenCode (show both)
  build: { visible: true, semantic: 'default' }
  // 'plan' already defined above
}

/**
 * Filter modes - only show modes explicitly configured with visible: true
 * Unknown modes are hidden (must be added to MODE_CONFIG first)
 */
export function filterVisibleModes(modes: SessionMode[]): SessionMode[] {
  return modes.filter((mode) => {
    const config = MODE_CONFIG[mode.id]
    // Unknown modes default to hidden
    return config?.visible ?? false
  })
}

/**
 * Get semantic type for UI rendering (defaults to 'default' for unknown modes)
 */
export function getSemanticType(modeId: string): SemanticType {
  return MODE_CONFIG[modeId]?.semantic ?? 'default'
}

/** Check if a mode is visible (unknown modes return false) */
export function isModeVisible(modeId: string): boolean {
  return MODE_CONFIG[modeId]?.visible ?? false
}

/** Get raw mode config */
export function getModeConfig(modeId: string): ModeConfig | undefined {
  return MODE_CONFIG[modeId]
}

/** Get next mode in cycle for Shift+Tab switching */
export function getNextModeId(modes: SessionMode[], currentModeId: string): string | null {
  const visibleModes = filterVisibleModes(modes)
  if (visibleModes.length === 0) return null

  const currentIndex = visibleModes.findIndex((m) => m.id === currentModeId)
  if (currentIndex === -1) return visibleModes[0].id

  const nextIndex = (currentIndex + 1) % visibleModes.length
  return visibleModes[nextIndex].id
}
