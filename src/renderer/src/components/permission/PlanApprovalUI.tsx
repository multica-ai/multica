/**
 * Plan approval UI - displays plan content with markdown rendering
 *
 * Design: Minimal, follows existing permission UI patterns
 * Key: Accept button first (primary action), clean hierarchy
 */
import { FileText } from 'lucide-react'
import { usePermissionStore } from '../../stores/permissionStore'
import { Button } from '@/components/ui/button'
import { Markdown } from '../markdown/Markdown'
import type { PlanApprovalUIProps } from './types'

/**
 * Categorize options into approve and deny groups
 * Falls back to showing all options if categorization fails
 */
function categorizeOptions(options: PlanApprovalUIProps['request']['options']): {
  approveOptions: typeof options
  denyOption: (typeof options)[0] | undefined
} {
  // Primary action: approve options (by kind first, then by name)
  const approveOptions = options.filter(
    (o) =>
      o.kind === 'allow_once' ||
      o.kind === 'allow' ||
      o.name?.toLowerCase().match(/yes|accept|approve|confirm|start/i)
  )

  // Secondary: keep planning / deny
  const denyOption = options.find(
    (o) =>
      o.kind === 'deny' ||
      o.kind === 'reject_once' ||
      o.name?.toLowerCase().match(/no|deny|reject|keep planning|cancel/i)
  )

  // Fallback: if no categorization worked, treat first as approve, last as deny
  if (approveOptions.length === 0 && options.length > 0) {
    return {
      approveOptions: [options[0]],
      denyOption: options.length > 1 ? options[options.length - 1] : undefined
    }
  }

  return { approveOptions, denyOption }
}

export function PlanApprovalUI({ request, planContent }: PlanApprovalUIProps): React.JSX.Element {
  const respondToRequest = usePermissionStore((s) => s.respondToRequest)
  const { options } = request

  const { approveOptions, denyOption } = categorizeOptions(options)

  // Normalize plan content - handle empty/whitespace
  const normalizedContent = planContent?.trim() || ''
  const hasContent = normalizedContent.length > 0

  return (
    <div className="rounded-lg border border-border bg-muted/30 p-3 space-y-3">
      {/* Header - consistent with StandardPermissionUI */}
      <div className="flex items-center gap-2">
        <FileText className="h-4 w-4 text-blue-500" />
        <span className="font-medium text-sm text-foreground">Plan Ready</span>
      </div>

      {/* Plan content */}
      {hasContent ? (
        <div className="rounded-md bg-muted/50 p-3 max-h-[50vh] overflow-y-auto scrollbar-thin">
          <Markdown mode="minimal">{normalizedContent}</Markdown>
        </div>
      ) : (
        <div className="rounded-md bg-muted/50 p-3 text-sm text-muted-foreground">
          Plan content not available. Please check the plan file.
        </div>
      )}

      {/* Action buttons - only first is primary, rest are outline */}
      <div className="flex flex-wrap gap-2">
        {approveOptions.map((option, index) => (
          <Button
            key={option.optionId}
            size="sm"
            variant={index === 0 ? 'default' : 'outline'}
            onClick={() => respondToRequest(option.optionId)}
          >
            {option.name || 'Approve'}
          </Button>
        ))}
        {denyOption && (
          <Button size="sm" variant="outline" onClick={() => respondToRequest(denyOption.optionId)}>
            {denyOption.name || 'Deny'}
          </Button>
        )}
      </div>
    </div>
  )
}
