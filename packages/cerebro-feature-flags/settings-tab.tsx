"use client";

import { Switch } from "@multica/ui/components/ui/switch";
import { Label } from "@multica/ui/components/ui/label";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { toast } from "sonner";
import { CEREBRO_FLAGS, type CerebroFlagKey } from "./registry";
import { useFeatureFlag, useFeatureFlagsQuery, useSetFeatureFlagMutation } from "./api";

function FlagRow({ flagKey, label, description }: { flagKey: CerebroFlagKey; label: string; description: string }) {
  const enabled = useFeatureFlag(flagKey);
  const mutation = useSetFeatureFlagMutation();

  const handleChange = (next: boolean) => {
    mutation.mutate(
      { key: flagKey, enabled: next },
      {
        onError: (err) => {
          toast.error(err instanceof Error ? err.message : `Failed to update ${label}`);
        },
      },
    );
  };

  return (
    <Card>
      <CardContent>
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <Label className="text-sm font-medium">{label}</Label>
            <p className="text-xs text-muted-foreground">{description}</p>
          </div>
          <Switch
            checked={enabled}
            disabled={mutation.isPending}
            onCheckedChange={handleChange}
            aria-label={label}
          />
        </div>
      </CardContent>
    </Card>
  );
}

export function CerebroFeatureFlagsTab() {
  const query = useFeatureFlagsQuery();

  return (
    <section className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-sm font-semibold">Cerebro features</h2>
        <p className="text-xs text-muted-foreground">
          Toggle cerebro-fork features for this workspace. Defaults are on; turning
          a feature off falls back to upstream multica behaviour where applicable.
        </p>
      </div>

      {query.isError && (
        <p className="text-xs text-destructive">
          Failed to load feature flags: {query.error instanceof Error ? query.error.message : "unknown error"}
        </p>
      )}

      <div className="space-y-3">
        {CEREBRO_FLAGS.map((flag) => (
          <FlagRow
            key={flag.key}
            flagKey={flag.key}
            label={flag.label}
            description={flag.description}
          />
        ))}
      </div>
    </section>
  );
}
