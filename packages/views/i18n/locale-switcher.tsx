"use client";

import { Languages } from "lucide-react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import { localeLabels, locales, useI18n, type Locale } from "./context";

export function LocaleSwitcher({
  className,
  size = "default",
}: {
  className?: string;
  size?: "sm" | "default";
}) {
  const { locale, setLocale, t } = useI18n();

  return (
    <Select value={locale} onValueChange={(value) => value && setLocale(value as Locale)}>
      <SelectTrigger
        size={size}
        aria-label={t("locale.language")}
        className={cn("w-[220px] max-w-full min-w-0 bg-background", className)}
      >
        <SelectValue>
          {(value: string | null) => (
            <span className="flex items-center gap-2">
              <Languages className="size-4 text-muted-foreground" />
              <span>{value ? localeLabels[value as Locale] : t("locale.language")}</span>
            </span>
          )}
        </SelectValue>
      </SelectTrigger>
      <SelectContent align="start" sideOffset={8}>
        {locales.map((candidate) => (
          <SelectItem key={candidate} value={candidate}>
            {localeLabels[candidate]}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
