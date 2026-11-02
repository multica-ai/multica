"use client";

import { LoaderCircle } from "@multica/ui/icons";
import { Card, CardContent, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { Switch } from "@multica/ui/components/ui/switch";
import type { EngineeringConfig } from "@multica/core/api";
import type { MemberWithUser } from "@multica/core/types";
import { useT } from "../../i18n";

interface EngineeringSettingsProps {
  config: EngineeringConfig;
  members: readonly MemberWithUser[];
  pending: boolean;
  onChange: (data: { enabled?: boolean; deciderMemberIds?: string[] }) => void;
}

export function EngineeringSettings({ config, members, pending, onChange }: EngineeringSettingsProps) {
  const { t } = useT("engineering");
  return (
    <Card data-engineering-region="settings">
      <CardHeader><CardTitle className="flex items-center gap-2">{pending ? <LoaderCircle className="size-4 animate-spin" aria-hidden="true" /> : null}{t(($) => $.settings.title)}</CardTitle></CardHeader>
      <CardContent className="space-y-5">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1"><label htmlFor="engineering-enabled" className="font-medium">{t(($) => $.settings.enabledLabel)}</label><p className="text-caption text-muted-foreground">{t(($) => $.settings.enabledHint)}</p></div>
          <Switch id="engineering-enabled" checked={config.enabled} disabled={pending} onCheckedChange={(enabled) => onChange({ enabled })} />
        </div>
        <fieldset className="space-y-2">
          <legend className="font-medium">{t(($) => $.settings.decidersLabel)}</legend>
          <p className="text-caption text-muted-foreground">{t(($) => $.settings.decidersHint)}</p>
          <div className="grid gap-2 sm:grid-cols-2">
            {members.map((member) => {
              const checked = config.deciderMemberIds.includes(member.user_id);
              return (
                <label key={member.id} className="flex items-center gap-2 rounded-md border p-2 text-caption">
                  <input
                    type="checkbox"
                    checked={checked}
                    disabled={pending}
                    onChange={() => {
                      const next = new Set(config.deciderMemberIds);
                      if (checked) next.delete(member.user_id); else next.add(member.user_id);
                      onChange({ deciderMemberIds: [...next] });
                    }}
                  />
                  <span>{member.name}</span>
                </label>
              );
            })}
          </div>
        </fieldset>
      </CardContent>
    </Card>
  );
}
