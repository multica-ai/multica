"use client";

import { useT, useLocale } from "@multica/i18n/react";
import { locales, localeLabels } from "@multica/i18n/types";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import { cn } from "@multica/ui/lib/utils";

const LIGHT_COLORS = {
  titleBar: "#e8e8e8",
  content: "#ffffff",
  sidebar: "#f4f4f5",
  bar: "#e4e4e7",
  barMuted: "#d4d4d8",
};

const DARK_COLORS = {
  titleBar: "#333338",
  content: "#27272a",
  sidebar: "#1e1e21",
  bar: "#3f3f46",
  barMuted: "#52525b",
};

function WindowMockup({
  variant,
  className,
}: {
  variant: "light" | "dark";
  className?: string;
}) {
  const colors = variant === "light" ? LIGHT_COLORS : DARK_COLORS;

  return (
    <div className={cn("flex h-full w-full flex-col", className)}>
      {/* Title bar */}
      <div
        className="flex items-center gap-[3px] px-2 py-1.5"
        style={{ backgroundColor: colors.titleBar }}
      >
        <span className="size-[6px] rounded-full bg-[#ff5f57]" />
        <span className="size-[6px] rounded-full bg-[#febc2e]" />
        <span className="size-[6px] rounded-full bg-[#28c840]" />
      </div>
      {/* Content area */}
      <div
        className="flex flex-1"
        style={{ backgroundColor: colors.content }}
      >
        {/* Sidebar */}
        <div
          className="w-[30%] space-y-1 p-2"
          style={{ backgroundColor: colors.sidebar }}
        >
          <div
            className="h-1 w-3/4 rounded-full"
            style={{ backgroundColor: colors.bar }}
          />
          <div
            className="h-1 w-1/2 rounded-full"
            style={{ backgroundColor: colors.bar }}
          />
        </div>
        {/* Main */}
        <div className="flex-1 space-y-1.5 p-2">
          <div
            className="h-1.5 w-4/5 rounded-full"
            style={{ backgroundColor: colors.bar }}
          />
          <div
            className="h-1 w-full rounded-full"
            style={{ backgroundColor: colors.barMuted }}
          />
          <div
            className="h-1 w-3/5 rounded-full"
            style={{ backgroundColor: colors.barMuted }}
          />
        </div>
      </div>
    </div>
  );
}

export function AppearanceTab() {
  const t = useT("settings");
  const { locale, setLocale } = useLocale();
  const { theme, setTheme } = useTheme();

  const themeOptions = [
    { value: "light" as const, label: t("light") },
    { value: "dark" as const, label: t("dark") },
    { value: "system" as const, label: t("system") },
  ];

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t("theme")}</h2>
        <div className="flex gap-6" role="radiogroup" aria-label={t("aria_radiogroup")}>
          {themeOptions.map((opt) => {
            const active = theme === opt.value;
            return (
              <button
                key={opt.value}
                role="radio"
                aria-checked={active}
                aria-label={
                  opt.value === "light"
                    ? t("aria_select_light")
                    : opt.value === "dark"
                      ? t("aria_select_dark")
                      : t("aria_select_system")
                }
                onClick={() => setTheme(opt.value)}
                className="group flex flex-col items-center gap-2"
              >
                <div
                  className={cn(
                    "aspect-[4/3] w-36 overflow-hidden rounded-lg ring-1 transition-all",
                    active
                      ? "ring-2 ring-brand"
                      : "ring-border hover:ring-2 hover:ring-border"
                  )}
                >
                  {opt.value === "system" ? (
                    <div className="relative h-full w-full">
                      <WindowMockup
                        variant="light"
                        className="absolute inset-0"
                      />
                      <WindowMockup
                        variant="dark"
                        className="absolute inset-0 [clip-path:inset(0_0_0_50%)]"
                      />
                    </div>
                  ) : (
                    <WindowMockup variant={opt.value} />
                  )}
                </div>
                <span
                  className={cn(
                    "text-sm transition-colors",
                    active
                      ? "font-medium text-foreground"
                      : "text-muted-foreground"
                  )}
                >
                  {opt.label}
                </span>
              </button>
            );
          })}
        </div>
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold">{t("language")}</h2>
        <p className="text-sm text-muted-foreground">{t("language_description")}</p>
        <div className="flex gap-2">
          {locales.map((l) => (
            <button
              key={l}
              onClick={() => setLocale(l)}
              className={cn(
                "rounded-md px-3 py-1.5 text-sm transition-colors",
                locale === l
                  ? "bg-primary text-primary-foreground"
                  : "bg-muted text-muted-foreground hover:bg-muted/80",
              )}
            >
              {localeLabels[l]}
            </button>
          ))}
        </div>
      </section>
    </div>
  );
}
