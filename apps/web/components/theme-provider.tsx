"use client"

// Re-export the shared ThemeProvider from @multica/ui
export { ThemeProvider } from "@multica/ui/components/common/theme-provider"

// Suppress React 19 false-positive about next-themes' inline <script>.
// The script works correctly; React 19 just warns about any <script> in components.
// See: https://github.com/pacocoursey/next-themes/issues/337
if (typeof window !== "undefined" && process.env.NODE_ENV === "development") {
  const orig = console.error;
  console.error = (...args: unknown[]) => {
    if (typeof args[0] === "string" && args[0].includes("Encountered a script tag"))
      return;
    orig.apply(console, args);
  };
}
