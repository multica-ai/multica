// CEREBRO-PATCH(autopilot-model-section): per-autopilot model override UI (JEH-1310).
//
// Net-new sibling component so the upstream autopilot-dialog only carries a
// thin `<CerebroAutopilotModelSection>` call plus a `model` field — the
// dropdown markup, registry list, and reset-to-default copy live here.
"use client";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";

// Keep the list in sync with the server-side autopilotmodel.Allowed() in
// server/internal/cerebro/autopilotmodel/model.go. Order = cheapest first so
// the cost-saving option is most visible.
export const CEREBRO_AUTOPILOT_MODELS = [
  { id: "claude-haiku-4-5-20251001", label: "Haiku 4.5" },
  { id: "claude-sonnet-4-6", label: "Sonnet 4.6" },
  { id: "claude-opus-4-7", label: "Opus 4.7" },
] as const;

// Sentinel separate from "" because Select treats "" as "no value".
const AGENT_DEFAULT = "__agent_default__";

interface Props {
  value: string | null | undefined;
  onChange: (value: string | null) => void;
}

export function CerebroAutopilotModelSection({ value, onChange }: Props) {
  const { t } = useT("autopilots");
  const current = value && value.length > 0 ? value : AGENT_DEFAULT;
  return (
    <div>
      <div className="text-[11px] font-semibold tracking-[0.08em] text-muted-foreground uppercase mb-2">
        {t(($) => $.dialog.cerebro_model.label)}
      </div>
      <Select
        value={current}
        onValueChange={(v) => onChange(v === AGENT_DEFAULT ? null : v)}
      >
        <SelectTrigger className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={AGENT_DEFAULT}>
            <span className="font-medium">
              {t(($) => $.dialog.cerebro_model.agent_default_label)}
            </span>
            <span className="block text-xs text-muted-foreground">
              {t(($) => $.dialog.cerebro_model.agent_default_hint)}
            </span>
          </SelectItem>
          {CEREBRO_AUTOPILOT_MODELS.map((m) => (
            <SelectItem key={m.id} value={m.id}>
              <span className="font-medium">{m.label}</span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <p className="mt-1.5 text-[11px] text-muted-foreground">
        {t(($) => $.dialog.cerebro_model.footnote)}
      </p>
    </div>
  );
}
