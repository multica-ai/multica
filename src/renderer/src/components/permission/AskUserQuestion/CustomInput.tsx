/**
 * Custom text input for free-form answers
 */
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { CustomInputProps } from '../types'

export function CustomInput({
  value,
  onChange,
  onSubmit,
  isMultiSelect = false
}: CustomInputProps) {
  // In multi-select mode, the Send button is hidden
  // User should use the Submit button in QuestionOptions instead
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !isMultiSelect) {
      onSubmit()
    }
  }

  return (
    <div className="flex gap-2">
      <Input
        placeholder={
          isMultiSelect ? 'Type custom text for "Other" option...' : 'Or type a custom answer...'
        }
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={handleKeyDown}
        className="text-sm"
      />
      {!isMultiSelect && (
        <Button size="sm" variant="outline" onClick={onSubmit} disabled={!value.trim()}>
          Send
        </Button>
      )}
    </div>
  )
}
