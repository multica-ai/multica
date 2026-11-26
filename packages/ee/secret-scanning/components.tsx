"use client";

import { useCallback, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, EyeOff, KeyRound, MoreHorizontal, RefreshCw, Shield, TriangleAlert } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueRouteRef, useWorkspacePaths } from "@multica/core/paths";
import { useCurrentMember, useIsGuest } from "@multica/core/permissions";
import { useWSEvent, useWSReconnect } from "@multica/core/realtime";
import type { WSEventType } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import type { SecretScanningExtension } from "@multica/views/extensions";
import { useTimeAgo } from "@multica/views/i18n";
import { useNavigation } from "@multica/views/navigation";
import {
  SettingsCard,
  SettingsReadOnlyNotice,
  SettingsRow,
  SettingsSection,
  SettingsTab,
  SettingsViewTabs,
} from "@multica/views/settings/layout";
import {
  findingsOptions,
  issueFindingsOptions,
  secretScanKeys,
  settingsOptions,
  useSetMode,
  useSettleFindings,
  useStartBackfill,
  type Finding,
  type ScanMode,
} from "./data";
import { useRuleLabel, useSecretScanT } from "./i18n";

type ListStatus = "open" | "redacted" | "dismissed";

function useIsAdmin(wsId: string): boolean {
  const { role } = useCurrentMember(wsId);
  return role === "owner" || role === "admin";
}

function ModeRow({ wsId, mode }: { wsId: string; mode: ScanMode }) {
  const t = useSecretScanT();
  const setMode = useSetMode(wsId);
  const items = [
    { value: "notify" as const, label: t("mode_notify") },
    { value: "redact" as const, label: t("mode_redact") },
  ];
  return (
    <SettingsRow label={t("mode_label")} description={t("mode_description")}>
      <Select items={items} value={mode} onValueChange={(value) => value && setMode.mutate(value as ScanMode)}>
        <SelectTrigger className="w-52" aria-label={t("mode_label")} disabled={setMode.isPending}>
          <SelectValue>{() => items.find((item) => item.value === mode)?.label}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          {items.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </SettingsRow>
  );
}

function HistoryRow({ wsId }: { wsId: string }) {
  const t = useSecretScanT();
  const timeAgo = useTimeAgo();
  const { data } = useQuery(settingsOptions(wsId, true));
  const start = useStartBackfill(wsId);
  const backfill = data?.backfill;
  const running = backfill?.status === "running" || start.isPending;
  let description = t("history_idle");
  if (backfill?.status === "running") description = t("history_running", { count: backfill.scanned });
  else if (backfill?.status === "done" && backfill.finished_at)
    description = t("history_done", { count: backfill.scanned, time: timeAgo(backfill.finished_at) });
  else if (backfill?.status === "failed") description = t("history_failed");
  return (
    <SettingsRow label={t("history_label")} description={description}>
      <Button variant="outline" size="sm" disabled={running} onClick={() => start.mutate()}>
        {running ? <Spinner className="size-3.5" /> : <RefreshCw className="size-3.5" />}
        {backfill?.status === "idle" ? t("scan_action") : t("rescan_action")}
      </Button>
    </SettingsRow>
  );
}

interface FindingGroup {
  fingerprint: string;
  findings: Finding[];
}

/** The same secret pasted into several comments is one row. */
function groupFindings(findings: Finding[]): FindingGroup[] {
  const groups = new Map<string, FindingGroup>();
  for (const finding of findings) {
    const group = groups.get(finding.fingerprint);
    if (group) group.findings.push(finding);
    else groups.set(finding.fingerprint, { fingerprint: finding.fingerprint, findings: [finding] });
  }
  return [...groups.values()];
}

function FindingRow({ wsId, group, status }: { wsId: string; group: FindingGroup; status: ListStatus }) {
  const t = useSecretScanT();
  const ruleLabel = useRuleLabel();
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const paths = useWorkspacePaths();
  const { push } = useNavigation();
  const settle = useSettleFindings(wsId);
  const first = group.findings[0]!;
  const ids = group.findings.map((f) => f.id);
  const count = new Set(group.findings.map((f) => f.comment_id)).size;

  const meta = [`${first.issue_identifier ?? ""} ${first.issue_title ?? ""}`.trim()];
  if (count === 1) meta.push(t("comment_by", { name: getActorName(first.author_type, first.author_id) }));
  meta.push(timeAgo(first.created_at));
  if (status === "redacted") {
    meta.push(
      first.resolved_by_type && first.resolved_by_id
        ? t("redacted_by", { name: getActorName(first.resolved_by_type, first.resolved_by_id) })
        : t("redacted_automatically"),
    );
  } else if (status === "dismissed" && first.resolved_by_type && first.resolved_by_id) {
    meta.push(t("dismissed_by", { name: getActorName(first.resolved_by_type, first.resolved_by_id) }));
  }

  const openComment = () =>
    push(`${paths.issueDetail(issueRouteRef({ id: first.issue_id, identifier: first.issue_identifier ?? null }))}#comment-${first.comment_id}`);

  return (
    <SettingsRow
      label={
        <span className="flex min-w-0 items-center gap-2">
          <KeyRound className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
          <span className="shrink-0 first-letter:uppercase">{ruleLabel(first.rule)}</span>
          <code className="min-w-0 truncate font-mono text-caption text-muted-foreground">{first.masked}</code>
          {count > 1 ? (
            <span className="shrink-0 rounded-full bg-muted px-2 text-caption text-muted-foreground">
              {t("in_comments", { count })}
            </span>
          ) : null}
        </span>
      }
      description={meta.filter(Boolean).join(" · ")}
    >
      <div className="flex items-center gap-1">
        {status === "open" ? (
          <Button
            variant="outline"
            size="sm"
            disabled={settle.isPending}
            onClick={() => settle.mutate({ action: "redact", ids })}
          >
            {t("redact_action")}
          </Button>
        ) : null}
        <DropdownMenu>
          <DropdownMenuTrigger render={<IconButton variant="ghost" size="icon-sm" label={t("more_actions")} />}>
            <MoreHorizontal />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-auto">
            <DropdownMenuItem onClick={openComment}>
              <ExternalLink />
              {t("view_comment")}
            </DropdownMenuItem>
            {status === "open" ? (
              <DropdownMenuItem onClick={() => settle.mutate({ action: "dismiss", ids })}>
                <EyeOff />
                {t("dismiss_action")}
              </DropdownMenuItem>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </SettingsRow>
  );
}

function FindingsSection({ wsId }: { wsId: string }) {
  const t = useSecretScanT();
  const [status, setStatus] = useState<ListStatus>("open");
  const { data, isPending } = useQuery(findingsOptions(wsId, status, true));
  const groups = useMemo(() => groupFindings(data?.findings ?? []), [data]);
  const counts = data?.counts;
  const empty = { open: t("empty_open"), redacted: t("empty_redacted"), dismissed: t("empty_dismissed") }[status];
  return (
    <SettingsSection
      title={t("section_findings")}
      description={t("findings_description")}
      action={
        <SettingsViewTabs<ListStatus>
          label={t("tabs_label")}
          value={status}
          onChange={setStatus}
          items={[
            { value: "open", label: t("tab_open"), count: counts?.open },
            { value: "redacted", label: t("tab_redacted"), count: counts?.redacted },
            { value: "dismissed", label: t("tab_dismissed"), count: counts?.dismissed },
          ]}
        />
      }
    >
      {isPending ? (
        <div className="flex justify-center py-8">
          <Spinner />
        </div>
      ) : groups.length === 0 ? (
        <p className="rounded-xl border border-dashed border-surface-border px-4 py-8 text-center text-caption text-muted-foreground">
          {empty}
        </p>
      ) : (
        <SettingsCard>
          {groups.map((group) => (
            <FindingRow key={group.fingerprint} wsId={wsId} group={group} status={status} />
          ))}
        </SettingsCard>
      )}
    </SettingsSection>
  );
}

function SettingsContent() {
  const t = useSecretScanT();
  const wsId = useWorkspaceId();
  const isAdmin = useIsAdmin(wsId);
  const { data } = useQuery(settingsOptions(wsId, isAdmin));
  return (
    <SettingsTab title={t("settings_label")} description={t("page_description")} scope="workspace">
      {isAdmin ? (
        <>
          <SettingsSection title={t("section_scanning")}>
            <SettingsCard>
              <ModeRow wsId={wsId} mode={data?.mode ?? "notify"} />
              <HistoryRow wsId={wsId} />
            </SettingsCard>
          </SettingsSection>
          <FindingsSection wsId={wsId} />
        </>
      ) : (
        <SettingsReadOnlyNotice wsId={wsId} />
      )}
    </SettingsTab>
  );
}

/** Under a comment with open findings, for its author and the admins. */
function CommentNotice({ issueId, commentId }: { issueId: string; commentId: string }) {
  const t = useSecretScanT();
  const ruleLabel = useRuleLabel();
  const wsId = useWorkspaceId();
  const isGuest = useIsGuest(wsId);
  const { data } = useQuery({ ...issueFindingsOptions(wsId, issueId), enabled: !isGuest && wsId.length > 0 });
  const settle = useSettleFindings(wsId);
  const findings = useMemo(() => (data ?? []).filter((f) => f.comment_id === commentId), [data, commentId]);
  if (findings.length === 0) return null;
  const ids = findings.map((f) => f.id);
  const kinds = [...new Set(findings.map((f) => ruleLabel(f.rule)))].join(t("kinds_separator"));
  return (
    <div
      role="status"
      className="mt-2 flex items-start gap-2.5 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-caption"
    >
      <TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-warning" aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <p className="font-medium text-foreground">{t("notice_title", { kinds })}</p>
        <p className="text-muted-foreground">{t("notice_hint")}</p>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <Button
          variant="ghost"
          size="xs"
          disabled={settle.isPending}
          onClick={() => settle.mutate({ action: "dismiss", ids })}
        >
          {t("dismiss_action")}
        </Button>
        <Button
          variant="outline"
          size="xs"
          disabled={settle.isPending}
          onClick={() => settle.mutate({ action: "redact", ids })}
        >
          {t("redact_action")}
        </Button>
      </div>
    </div>
  );
}

const REFRESH_EVENTS = ["secret_scan:changed", "comment:deleted", "issue:deleted"] as WSEventType[];

function RefreshOn({ event, refresh }: { event: WSEventType; refresh: () => void }) {
  useWSEvent(event, refresh);
  return null;
}

/** Keeps findings in sync with comments written, edited and removed elsewhere. */
function Host() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const refresh = useCallback(() => {
    void qc.invalidateQueries({ queryKey: secretScanKeys.all(wsId) });
  }, [qc, wsId]);
  useWSReconnect(refresh);
  return (
    <>
      {REFRESH_EVENTS.map((event) => (
        <RefreshOn key={event} event={event} refresh={refresh} />
      ))}
    </>
  );
}

function useSettingsLabel(): string {
  return useSecretScanT()("settings_label");
}

export const secretScanningExtension: SecretScanningExtension = {
  settings: { value: "security", icon: Shield, useLabel: useSettingsLabel, Content: SettingsContent },
  CommentNotice,
  Host,
};
