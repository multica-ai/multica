import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { parseWithFallback } from "@multica/core/api/schema";
import { Button } from "@multica/ui/components/ui/button";
import { SettingsCard, SettingsRow, SettingsSection, SettingsTab } from "@multica/views/settings";
import { useT } from "@multica/views/i18n";
import { computerUseApprovalsSchema, type ComputerUseApprovals } from "../../../shared/computer-use-approvals";

const key = ["computer-use", "approvals"] as const;
const none: ComputerUseApprovals = { available: false, apps: [] };
const parse = (value: unknown) =>
  parseWithFallback(value, computerUseApprovalsSchema, none, { endpoint: "desktop.computer-use.approvals" });

export function ComputerUseSettingsTab() {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const approvals = useQuery({
    queryKey: key,
    queryFn: async () => parse(await window.desktopAPI.computerUseApprovals.list()),
  });
  const change = useMutation({
    mutationFn: async (appId: string | null) =>
      parse(await (appId === null
        ? window.desktopAPI.computerUseApprovals.clear()
        : window.desktopAPI.computerUseApprovals.remove(appId))),
    onSuccess: (value) => qc.setQueryData(key, value),
  });
  const apps = approvals.data?.apps ?? [];
  return (
    <SettingsTab title={t(($) => $.desktop.computer_use.title)} scope="device">
      <SettingsSection
        title={t(($) => $.desktop.computer_use.apps)}
        description={t(($) => $.desktop.computer_use.apps_description)}
        action={apps.length > 1 ? (
          <Button variant="outline" size="sm" disabled={change.isPending} onClick={() => change.mutate(null)}>
            {t(($) => $.desktop.computer_use.clear)}
          </Button>
        ) : null}
      >
        <SettingsCard>
          {apps.length === 0 && !approvals.isPending ? (
            <p className="px-4 py-3 text-caption text-muted-foreground">{t(($) => $.desktop.computer_use.empty)}</p>
          ) : null}
          {apps.map((app) => (
            <SettingsRow key={app.id} label={app.name} description={<span className="break-all">{app.id}</span>}>
              <Button
                variant="outline"
                size="sm"
                disabled={change.isPending}
                aria-label={t(($) => $.desktop.computer_use.remove_label, { name: app.name })}
                onClick={() => change.mutate(app.id)}
              >
                {t(($) => $.desktop.computer_use.remove)}
              </Button>
            </SettingsRow>
          ))}
          {(approvals.isError || change.isError) && (
            <p role="alert" className="px-4 pb-3 text-caption text-destructive">{t(($) => $.desktop.computer_use.failed)}</p>
          )}
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}
