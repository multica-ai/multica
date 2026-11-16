"use client"

import { useTheme } from "next-themes"
import { Toaster as Sonner, type ToasterProps } from "sonner"
import { CircleCheckIcon, InfoIcon, TriangleAlertIcon, OctagonXIcon, Loader2Icon } from "@multica/ui/icons"

// Sonner's default gaps (24px, 16px on phones), measured from the safe area
// rather than the screen edge so a toast never lands under a camera or the
// home indicator.
const safeAreaOffset = (gap: string) => ({
  top: `calc(${gap} + env(safe-area-inset-top))`,
  right: `calc(${gap} + env(safe-area-inset-right))`,
  bottom: `calc(${gap} + env(safe-area-inset-bottom))`,
  left: `calc(${gap} + env(safe-area-inset-left))`,
})

const Toaster = ({ ...props }: ToasterProps) => {
  // Use `resolvedTheme` (the concrete "light" / "dark" value) instead of
  // `theme` (which can be "system"). When we forward "system", sonner reads
  // `prefers-color-scheme` itself, and the Electron renderer's media query
  // can disagree with next-themes' `html.dark` class — that's why the toast
  // sometimes rendered light on a dark UI.
  const { resolvedTheme = "system" } = useTheme()

  // Sonner forwards no data attributes, so the theme anchor sits on a
  // box-less wrapper; the toast list inside stays `position: fixed`.
  return (
    <div data-mc="toaster" className="contents">
      <Sonner
        theme={resolvedTheme as ToasterProps["theme"]}
        className="toaster group"
        offset={safeAreaOffset("24px")}
        mobileOffset={safeAreaOffset("16px")}
        icons={{
          success: (
            <CircleCheckIcon className="size-4 text-success" />
          ),
          info: (
            <InfoIcon className="size-4 text-info" />
          ),
          warning: (
            <TriangleAlertIcon className="size-4 text-warning" />
          ),
          error: (
            <OctagonXIcon className="size-4 text-destructive" />
          ),
          loading: (
            <Loader2Icon className="size-4 animate-spin text-brand" />
          ),
        }}
        style={
          {
            "--normal-bg": "var(--popover)",
            "--normal-text": "var(--popover-foreground)",
            "--normal-border": "var(--border)",
            "--border-radius": "var(--radius)",
          } as React.CSSProperties
        }
        toastOptions={{
          classNames: {
            toast: "cn-toast",
          },
        }}
        {...props}
      />
    </div>
  )
}

export { Toaster }
