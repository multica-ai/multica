import * as React from "react"
import { Input as InputPrimitive } from "@base-ui/react/input"

import { cn } from "@multica/ui/lib/utils"

// The `underline` variant strips the box down to a single bottom border —
// a borderless field that reads as page text until focused. Used by inline
// edit surfaces (space create/detail, popover search) instead of copy-pasting
// the override string.
const inputVariantClasses = {
  // Borderless: a single bottom rule, transparent even when disabled, so a
  // locked field reads as muted text rather than a filled grey box.
  underline:
    "rounded-none border-0 border-b px-0 shadow-none focus-visible:ring-0 focus-visible:border-foreground bg-transparent disabled:bg-transparent dark:bg-transparent dark:disabled:bg-transparent",
} as const

function Input({
  className,
  type,
  variant,
  ...props
}: React.ComponentProps<"input"> & { variant?: "underline" }) {
  return (
    <InputPrimitive
      type={type}
      data-slot="input"
      className={cn(
        "h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none file:inline-flex file:h-6 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:cursor-not-allowed disabled:bg-input/50 disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 md:text-sm dark:bg-input/30 dark:disabled:bg-input/80 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40",
        variant && inputVariantClasses[variant],
        className
      )}
      {...props}
    />
  )
}

export { Input }
