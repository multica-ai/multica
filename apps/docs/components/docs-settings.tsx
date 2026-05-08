"use client";

import { Monitor, Moon, Sun } from "lucide-react";
import { useTheme } from "next-themes";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { i18n } from "@/lib/i18n";
import { localeLabels } from "@/lib/translations";

// Sidebar-footer chrome: a language switch on the left and a theme switch
// on the right. Replaces Fumadocs's default icon-only row, which buried
// the language option behind a tiny globe. Each control shows the current
// value as a label so the affordance is obvious at a glance.

const BASE_PATH = "/docs";

function switchLocalePath(pathname: string, target: string): string {
  // Next strips basePath before the router, so `pathname` starts at `/`
  // or `/<locale>/...`. Default-locale URLs are prefix-less.
  const segments = pathname.split("/").filter(Boolean);
  const first = segments[0];
  const hasLocalePrefix =
    first && i18n.languages.some((l) => l === first && l !== i18n.defaultLanguage);

  const rest = hasLocalePrefix ? segments.slice(1) : segments;
  const prefixed =
    target === i18n.defaultLanguage ? rest : [target, ...rest];

  return "/" + prefixed.join("/");
}

const THEME_OPTIONS: { value: string; label: string; icon: ReactNode }[] = [
  { value: "light", label: "Light", icon: <Sun className="size-4" /> },
  { value: "dark", label: "Dark", icon: <Moon className="size-4" /> },
  { value: "system", label: "System", icon: <Monitor className="size-4" /> },
];

export function DocsSettings({ locale }: { locale: string }) {
  const router = useRouter();
  const pathname = usePathname();
  const { theme, setTheme } = useTheme();

  // Gate theme reads until mount — next-themes is SSR-incompatible and
  // would otherwise cause a hydration flash of the wrong icon.
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  const activeTheme = mounted ? (theme ?? "system") : "system";
  const activeThemeOption =
    THEME_OPTIONS.find((o) => o.value === activeTheme) ?? THEME_OPTIONS[2]!;

  const handleLocaleChange = (next: string) => {
    if (next === locale) return;
    const internal = pathname.startsWith(BASE_PATH)
      ? pathname.slice(BASE_PATH.length) || "/"
      : pathname;
    router.push(switchLocalePath(internal, next));
  };

  return (
    <div className="flex w-full items-center justify-end gap-2">
      {/* Language — left pill. Shows current language name. */}
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="sm"
              className="font-normal text-muted-foreground"
              aria-label="Switch language"
            >
              {localeLabels[locale as keyof typeof localeLabels] ?? locale}
            </Button>
          }
        />
        <DropdownMenuContent align="start" side="top" className="min-w-[140px]">
          {i18n.languages.map((lang) => (
            <DropdownMenuItem
              key={lang}
              onClick={() => handleLocaleChange(lang)}
              className={cn(lang === locale && "bg-accent")}
            >
              {localeLabels[lang as keyof typeof localeLabels]}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Theme — right icon button. Matched height to the sm pill via
          the icon-sm size token; without this the icon variant defaults
          to 32px while size="sm" is 28px, misaligning them. */}
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              className="shrink-0 text-muted-foreground"
              aria-label="Switch theme"
            >
              {activeThemeOption.icon}
            </Button>
          }
        />
        <DropdownMenuContent align="end" side="top" className="min-w-[140px]">
          {THEME_OPTIONS.map((opt) => (
            <DropdownMenuItem
              key={opt.value}
              onClick={() => setTheme(opt.value)}
              className={cn(
                "gap-2",
                opt.value === activeTheme && "bg-accent",
              )}
            >
              {opt.icon}
              {opt.label}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
