/**
 * Agent setup / Settings component
 * Using shadcn/ui components
 */
import { useState, useEffect } from 'react'
import type { AgentCheckResult } from '../../../shared/electron-api'
import { useTheme } from '../contexts/ThemeContext'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Sun, Moon, Monitor, Check, Loader2, RefreshCw } from 'lucide-react'

interface SettingsProps {
  isOpen: boolean
  onClose: () => void
  currentAgentId: string | null
  onSwitchAgent: (agentId: string) => Promise<void>
}

// Agent icons mapping
const AGENT_ICONS: Record<string, string> = {
  'claude-code': '◉',
  opencode: '⌘',
  codex: '◈',
  gemini: '✦',
}

type ThemeMode = 'light' | 'dark' | 'system'

export function Settings({ isOpen, onClose, currentAgentId, onSwitchAgent }: SettingsProps) {
  const [agents, setAgents] = useState<AgentCheckResult[]>([])
  const [loading, setLoading] = useState(true)
  const [switching, setSwitching] = useState<string | null>(null)
  const [selectedAgent, setSelectedAgent] = useState<string | null>(null)
  const { mode, setMode } = useTheme()

  useEffect(() => {
    if (isOpen) {
      setSelectedAgent(currentAgentId)
      loadAgents()
    }
  }, [isOpen, currentAgentId])

  useEffect(() => {
    if (agents.length > 0 && !selectedAgent) {
      const firstInstalled = agents.find(a => a.installed)
      if (firstInstalled) {
        setSelectedAgent(firstInstalled.id)
      }
    }
  }, [agents, selectedAgent])

  async function loadAgents() {
    setLoading(true)
    try {
      const results = await window.electronAPI.checkAgents()
      setAgents(results)
    } catch (err) {
      console.error('Failed to check agents:', err)
    } finally {
      setLoading(false)
    }
  }

  async function handleContinue() {
    if (!selectedAgent || switching) return

    if (selectedAgent === currentAgentId) {
      onClose()
      return
    }

    setSwitching(selectedAgent)
    try {
      await onSwitchAgent(selectedAgent)
      onClose()
    } catch (err) {
      console.error('Failed to switch agent:', err)
    } finally {
      setSwitching(null)
    }
  }

  const installedCount = agents.filter(a => a.installed).length

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-md max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="text-2xl">Settings</DialogTitle>
        </DialogHeader>

        {/* Appearance Section */}
        <div className="space-y-3">
          <h2 className="text-sm font-medium text-muted-foreground">Appearance</h2>
          <ToggleGroup
            type="single"
            value={mode}
            onValueChange={(value) => value && setMode(value as ThemeMode)}
            className="w-full"
          >
            <ToggleGroupItem value="light" className="flex-1 gap-2">
              <Sun className="h-4 w-4" />
              Light
            </ToggleGroupItem>
            <ToggleGroupItem value="dark" className="flex-1 gap-2">
              <Moon className="h-4 w-4" />
              Dark
            </ToggleGroupItem>
            <ToggleGroupItem value="system" className="flex-1 gap-2">
              <Monitor className="h-4 w-4" />
              System
            </ToggleGroupItem>
          </ToggleGroup>
        </div>

        {/* Agent Section */}
        <div className="space-y-3">
          <h2 className="text-sm font-medium text-muted-foreground">Coding Agent</h2>
          <p className="text-xs text-muted-foreground">
            Select a coding agent. Agents use local authentication.
          </p>

          {loading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <div className="space-y-3">
              {agents.map((agent) => (
                <AgentCard
                  key={agent.id}
                  agent={agent}
                  isSelected={agent.id === selectedAgent}
                  onSelect={() => agent.installed && setSelectedAgent(agent.id)}
                />
              ))}
            </div>
          )}
        </div>

        {/* Footer */}
        <DialogFooter className="flex-row items-center justify-between border-t pt-4 sm:justify-between">
          <Button
            variant="ghost"
            size="sm"
            onClick={loadAgents}
            disabled={loading}
          >
            <RefreshCw className="h-4 w-4" />
            Refresh
          </Button>

          <div className="flex gap-2">
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button
              onClick={handleContinue}
              disabled={!selectedAgent || !!switching || installedCount === 0}
            >
              {switching ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Switching...
                </>
              ) : (
                'Done'
              )}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

interface AgentCardProps {
  agent: AgentCheckResult
  isSelected: boolean
  onSelect: () => void
}

function AgentCard({ agent, isSelected, onSelect }: AgentCardProps) {
  const icon = AGENT_ICONS[agent.id] || '◇'

  return (
    <Card
      onClick={onSelect}
      className={`flex-row items-start gap-4 p-4 cursor-pointer transition-all ${
        !agent.installed
          ? 'opacity-50 cursor-not-allowed'
          : isSelected
            ? 'border-primary bg-primary/5'
            : 'hover:border-muted-foreground'
      }`}
    >
      {/* Icon */}
      <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-muted text-lg">
        {icon}
      </div>

      {/* Content */}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-medium">{agent.name}</span>
          {agent.installed && (
            <Badge variant="secondary" className="gap-1">
              <Check className="h-3 w-3" />
              installed
            </Badge>
          )}
        </div>
        <p className="mt-0.5 text-sm text-muted-foreground">
          {getAgentDescription(agent.id)}
        </p>
        {!agent.installed && agent.installHint && (
          <p className="mt-2 font-mono text-xs text-muted-foreground">
            {agent.installHint}
          </p>
        )}
      </div>

      {/* Selection indicator */}
      <div className={`flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full border-2 transition-colors ${
        isSelected
          ? 'border-primary bg-primary'
          : 'border-muted-foreground/30'
      }`}>
        {isSelected && <Check className="h-3 w-3 text-primary-foreground" />}
      </div>
    </Card>
  )
}

function getAgentDescription(agentId: string): string {
  const descriptions: Record<string, string> = {
    'claude-code': 'Anthropic\'s Claude Code via ACP',
    opencode: 'Terminal-based coding assistant',
    codex: 'OpenAI\'s Codex CLI via ACP',
    gemini: 'Google\'s Gemini CLI agent',
  }
  return descriptions[agentId] || 'Coding assistant'
}
