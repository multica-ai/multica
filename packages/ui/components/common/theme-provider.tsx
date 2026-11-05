import { ThemeProvider as NextThemesProvider, useTheme } from "next-themes";

export { useTheme };
import { TooltipProvider } from "../ui/tooltip";

export function ThemeProvider({
  children,
  ...props
}: React.ComponentProps<typeof NextThemesProvider>) {
  return (
    <NextThemesProvider
      attribute="class"
      defaultTheme="system"
      themes={["light", "dark", "blue-white"]}
      enableSystem
      disableTransitionOnChange
      {...props}
    >
      <TooltipProvider>
        {children}
      </TooltipProvider>
    </NextThemesProvider>
  );
}
