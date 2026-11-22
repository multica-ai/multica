"use client";

import { Cpu, Sparkles } from "@multica/ui/icons";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";

export function RuntimeStrategySection({
  shareRuntime,
  onShareRuntimeChange,
}: {
  shareRuntime: boolean;
  onShareRuntimeChange: (next: boolean) => void;
}) {
  const { t } = useT("agents");
  const label = t(($) => $.create_dialog.share_runtime.label);
  const options = [
    {
      value: false,
      icon: Sparkles,
      title: t(($) => $.create_dialog.share_runtime.smart_title),
      description: t(($) => $.create_dialog.share_runtime.smart_desc),
    },
    {
      value: true,
      icon: Cpu,
      title: t(($) => $.create_dialog.share_runtime.fixed_title),
      description: t(($) => $.create_dialog.share_runtime.fixed_desc),
    },
  ];

  return (
    <div>
      <Label className="text-caption text-muted-foreground">{label}</Label>
      <fieldset
        className="mt-1.5 grid grid-cols-1 gap-2 sm:grid-cols-2"
        role="radiogroup"
        aria-label={label}
      >
        <legend className="sr-only">{label}</legend>
        {options.map((option) => {
          const selected = shareRuntime === option.value;
          const Icon = option.icon;
          return (
            <button
              key={option.title}
              type="button"
              role="radio"
              aria-checked={selected}
              onClick={() => onShareRuntimeChange(option.value)}
              className={`flex min-h-[72px] w-full items-start gap-2 rounded-lg border px-3 py-2.5 text-body transition-colors ${
                selected
                  ? "border-primary bg-primary/5"
                  : "border-border hover:bg-muted"
              }`}
            >
              <Icon
                className="mt-0.5 size-4 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
              <span className="min-w-0 text-left">
                <span className="block font-medium">{option.title}</span>
                <span className="mt-0.5 block text-caption leading-snug text-muted-foreground">
                  {option.description}
                </span>
              </span>
            </button>
          );
        })}
      </fieldset>
    </div>
  );
}
