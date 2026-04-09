"use client"

import { ThemeProvider as NextThemesProvider } from "next-themes"
import { TooltipProvider } from "@multica/ui/components/ui/tooltip"

function ThemeProvider({
  children,
  ...props
}: React.ComponentProps<typeof NextThemesProvider>) {
  return (
    <NextThemesProvider
      attribute="class"
      defaultTheme="system"
      enableSystem
      disableTransitionOnChange
      {...props}
    >
      <TooltipProvider delay={500}>
        {children}
      </TooltipProvider>
    </NextThemesProvider>
  )
}

export { ThemeProvider }
