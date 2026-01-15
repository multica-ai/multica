import {
  CircleCheckIcon,
  InfoIcon,
  Loader2Icon,
  OctagonXIcon,
  TriangleAlertIcon
} from 'lucide-react'
import { useTheme } from '@/contexts/ThemeContext'
import { Toaster as Sonner, type ToasterProps } from 'sonner'

const Toaster = ({ ...props }: ToasterProps) => {
  const { resolvedTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'

  return (
    <Sonner
      theme={resolvedTheme as ToasterProps['theme']}
      className="toaster group"
      richColors
      icons={{
        success: <CircleCheckIcon className="size-4" />,
        info: <InfoIcon className="size-4" />,
        warning: <TriangleAlertIcon className="size-4" />,
        error: <OctagonXIcon className="size-4" />,
        loading: <Loader2Icon className="size-4 animate-spin" />
      }}
      style={
        {
          // Normal toast
          '--normal-bg': 'var(--popover)',
          '--normal-text': 'var(--popover-foreground)',
          '--normal-border': 'var(--border)',
          // Error toast - red styling
          '--error-bg': isDark ? 'oklch(0.25 0.08 25)' : 'oklch(0.97 0.02 25)',
          '--error-text': isDark ? 'oklch(0.9 0.1 25)' : 'oklch(0.45 0.18 25)',
          '--error-border': isDark ? 'oklch(0.45 0.15 25)' : 'oklch(0.7 0.15 25)',
          // Success toast - green styling
          '--success-bg': isDark ? 'oklch(0.25 0.06 145)' : 'oklch(0.97 0.02 145)',
          '--success-text': isDark ? 'oklch(0.85 0.12 145)' : 'oklch(0.4 0.12 145)',
          '--success-border': isDark ? 'oklch(0.45 0.1 145)' : 'oklch(0.7 0.1 145)',
          // Warning toast - amber styling
          '--warning-bg': isDark ? 'oklch(0.28 0.08 70)' : 'oklch(0.97 0.03 70)',
          '--warning-text': isDark ? 'oklch(0.9 0.12 70)' : 'oklch(0.45 0.14 70)',
          '--warning-border': isDark ? 'oklch(0.5 0.12 70)' : 'oklch(0.75 0.12 70)',
          '--border-radius': 'var(--radius)'
        } as React.CSSProperties
      }
      {...props}
    />
  )
}

export { Toaster }
