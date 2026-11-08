"use client";

import { useId, type ReactNode } from "react";
import { Check } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import { useAppearanceStore, type AppearanceStyle } from "@multica/core/appearance";
import { useT } from "../../i18n";
import { SettingsSection, SettingsTab } from "./settings-layout";

type PreviewMode = "light" | "dark";
type ThemeChoice = PreviewMode | "system";

const THEME_CHOICES: readonly ThemeChoice[] = ["light", "dark", "system"];
const STYLE_CHOICES: readonly AppearanceStyle[] = ["default", "pop"];

/**
 * Theme and style, each picked from miniature previews. Every preview
 * combines its own option with the other current choice, so a card shows
 * what the app will look like after clicking it.
 */
export function AppearanceTab() {
  const { t } = useT("settings");
  return (
    <SettingsTab title={t(($) => $.page.tabs.appearance)} scope="device">
      <ThemeSection />
      <StyleSection />
    </SettingsTab>
  );
}

function useAppliedMode(): PreviewMode {
  const { resolvedTheme } = useTheme();
  return resolvedTheme === "dark" ? "dark" : "light";
}

function ThemeSection() {
  const { t } = useT("settings");
  const { theme, setTheme } = useTheme();
  const style = useAppearanceStore((s) => s.style);
  const value: ThemeChoice = theme === "light" || theme === "dark" ? theme : "system";
  const title = t(($) => $.appearance.theme.title);
  return (
    <SettingsSection
      title={title}
      description={t(($) => $.appearance.theme.description)}
      anchor="theme"
    >
      <div role="group" aria-label={title} className="grid grid-cols-3 gap-4">
        {THEME_CHOICES.map((choice) => (
          <OptionCard
            key={choice}
            label={t(($) => $.appearance.theme[choice])}
            selected={value === choice}
            onSelect={() => setTheme(choice)}
            preview={
              choice === "system" ? (
                <div className="relative">
                  <AppearancePreview mode="light" style={style} />
                  <AppearancePreview
                    mode="dark"
                    style={style}
                    className="absolute inset-0 [clip-path:polygon(55%_0,100%_0,100%_100%,45%_100%)]"
                  />
                </div>
              ) : (
                <AppearancePreview mode={choice} style={style} />
              )
            }
          />
        ))}
      </div>
    </SettingsSection>
  );
}

function StyleSection() {
  const { t } = useT("settings");
  const mode = useAppliedMode();
  const style = useAppearanceStore((s) => s.style);
  const setStyle = useAppearanceStore((s) => s.setStyle);
  const title = t(($) => $.appearance.style.title);
  return (
    <SettingsSection
      title={title}
      description={t(($) => $.appearance.style.description)}
      anchor="style"
    >
      <div role="group" aria-label={title} className="grid gap-4 sm:grid-cols-2">
        {STYLE_CHOICES.map((choice) => (
          <OptionCard
            key={choice}
            label={t(($) => $.appearance.style[choice])}
            description={
              choice === "pop"
                ? t(($) => $.appearance.style.pop_description)
                : t(($) => $.appearance.style.default_description)
            }
            selected={style === choice}
            onSelect={() => setStyle(choice)}
            preview={<AppearancePreview mode={mode} style={choice} />}
          />
        ))}
      </div>
    </SettingsSection>
  );
}

function OptionCard({
  label,
  description,
  selected,
  onSelect,
  preview,
}: {
  label: string;
  description?: string;
  selected: boolean;
  onSelect: () => void;
  preview: ReactNode;
}) {
  const descriptionId = useId();
  return (
    <button
      type="button"
      aria-label={label}
      aria-describedby={description ? descriptionId : undefined}
      aria-pressed={selected}
      onClick={() => {
        if (!selected) onSelect();
      }}
      className="group/option flex min-w-0 flex-col gap-2.5 rounded-xl text-left outline-none"
    >
      <span
        className={cn(
          "@container block w-full overflow-hidden rounded-lg ring-1 ring-surface-border transition-shadow group-focus-visible/option:outline-2 group-focus-visible/option:outline-offset-2 group-focus-visible/option:outline-ring",
          selected
            ? "ring-2 ring-foreground"
            : "group-hover/option:ring-foreground/30",
        )}
      >
        {preview}
      </span>
      <span className="flex min-w-0 items-start gap-2 px-0.5">
        <span
          aria-hidden="true"
          className={cn(
            "mt-0.5 flex size-3.5 shrink-0 items-center justify-center rounded-full border",
            selected ? "border-foreground bg-foreground text-background" : "border-input",
          )}
        >
          {selected ? <Check className="size-2.5" /> : null}
        </span>
        <span className="min-w-0">
          <span className={cn("block truncate text-body", selected && "font-medium")}>
            {label}
          </span>
          {description ? (
            <span
              id={descriptionId}
              className="mt-0.5 block text-caption leading-5 text-muted-foreground"
            >
              {description}
            </span>
          ) : null}
        </span>
      </span>
    </button>
  );
}

/** A miniature app: rail, sidebar, and a list with two status groups. */
function AppearancePreview({
  mode,
  style,
  className,
}: {
  mode: PreviewMode;
  style: AppearanceStyle;
  className?: string;
}) {
  return (
    <span
      aria-hidden="true"
      data-appearance-preview=""
      data-preview-mode={mode}
      data-preview-style={style}
      className={cn(
        "flex aspect-[16/10] w-full gap-[2.5cqw] bg-(--pv-shell) p-[2.5cqw] pl-[2cqw]",
        className,
      )}
    >
      <span className="flex w-[5cqw] shrink-0 flex-col items-center gap-[2.5cqw] pt-[0.5cqw]">
        <span className="size-[4.5cqw] rounded-[1.2cqw] bg-(--pv-canvas)" />
        {[0, 1, 2].map((i) => (
          <span key={i} className="size-[3.2cqw] rounded-[0.9cqw] bg-(--pv-rail)" />
        ))}
      </span>
      <span className="flex min-w-0 flex-1 overflow-hidden rounded-(--pv-radius) border border-(--pv-line) bg-(--pv-canvas)">
        <span className="flex w-[24cqw] shrink-0 flex-col gap-[2.2cqw] border-r border-(--pv-line) p-[2.8cqw]">
          <span className="h-[2cqw] w-3/5 rounded-full bg-(--pv-ink)" />
          <span className="h-[4.2cqw] rounded-[0.9cqw] bg-(--pv-selected)" />
          {["w-4/5", "w-3/5", "w-2/3"].map((width) => (
            <span key={width} className={cn("h-[1.6cqw] rounded-full bg-(--pv-faint)", width)} />
          ))}
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-[2.2cqw] p-[3cqw]">
          <span className="flex items-center justify-between">
            <span className="h-[2cqw] w-[28%] rounded-full bg-(--pv-ink)" />
            <span className="h-[3.6cqw] w-[11cqw] rounded-[0.9cqw] border border-(--pv-primary-line) bg-(--pv-primary) shadow-(--pv-primary-shadow)" />
          </span>
          <PreviewGroup band="a" rows={["w-3/4", "w-1/2"]} />
          <PreviewGroup band="b" rows={["w-2/3", "w-3/5"]} />
        </span>
      </span>
    </span>
  );
}

function PreviewGroup({ band, rows }: { band: "a" | "b"; rows: string[] }) {
  return (
    <>
      <span
        className={cn(
          "flex h-[4.2cqw] items-center gap-[1.2cqw] rounded-[0.9cqw] px-[1.4cqw] ring-1 ring-inset ring-(--pv-band-line)",
          band === "a" ? "bg-(--pv-band-a)" : "bg-(--pv-band-b)",
        )}
      >
        <span
          className={cn(
            "size-[2cqw] rounded-full",
            band === "a" ? "bg-(--pv-dot-a)" : "bg-(--pv-dot-b)",
          )}
        />
        <span className="h-[1.6cqw] w-[22%] rounded-full bg-(--pv-band-ink)" />
      </span>
      {rows.map((width) => (
        <span key={width} className="flex items-center gap-[1.4cqw] px-[1.4cqw]">
          <span className="h-[1.6cqw] w-[9%] rounded-full bg-(--pv-faint)" />
          <span className={cn("h-[1.6cqw] rounded-full bg-(--pv-faint)", width)} />
        </span>
      ))}
    </>
  );
}
