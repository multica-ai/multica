"use client";

import { useEffect, useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";
import { getJiraBridge, useJiraSync } from "../jira/use-jira-sync";

interface JiraFormState {
  siteUrl: string;
  email: string;
  apiToken: string;
  jql: string;
  pollIntervalMinutes: number;
  statusMappingText: string;
}

const EMPTY_FORM: JiraFormState = {
  siteUrl: "",
  email: "",
  apiToken: "",
  jql: "assignee = currentUser() ORDER BY updated DESC",
  pollIntervalMinutes: 0,
  statusMappingText: "{}",
};

/** Jira → Multica one-way sync settings. Desktop-only: the Jira REST calls run
 *  in the Electron main process (via the desktop bridge) to bypass browser
 *  CORS, so on web we render a notice instead of the form. */
export function JiraTab() {
  const { t } = useT("settings");
  const isDesktop = !!getJiraBridge();
  const { syncNow, running, lastResult, error } = useJiraSync();
  const [form, setForm] = useState<JiraFormState>(EMPTY_FORM);
  const [hasToken, setHasToken] = useState(false);
  const [saveMessage, setSaveMessage] = useState<string | null>(null);

  useEffect(() => {
    const bridge = getJiraBridge();
    if (!bridge) return;
    void bridge.getConfig().then((cfg) => {
      setForm({
        siteUrl: cfg.siteUrl,
        email: cfg.email,
        apiToken: "",
        jql: cfg.jql,
        pollIntervalMinutes: cfg.pollIntervalMinutes,
        statusMappingText: JSON.stringify(cfg.statusMapping ?? {}, null, 2),
      });
      setHasToken(cfg.hasToken === true);
    });
  }, []);

  if (!isDesktop) {
    return (
      <Card>
        <CardContent className="p-4 text-sm text-muted-foreground">
          {t(($) => $.jira.desktop_only)}
        </CardContent>
      </Card>
    );
  }

  const onSave = async () => {
    const bridge = getJiraBridge();
    if (!bridge) return;
    setSaveMessage(null);
    let statusMapping: Record<string, string>;
    try {
      statusMapping = JSON.parse(form.statusMappingText || "{}") as Record<string, string>;
    } catch {
      setSaveMessage(t(($) => $.jira.status_map_invalid));
      return;
    }
    await bridge.setConfig({
      siteUrl: form.siteUrl.trim(),
      email: form.email.trim(),
      // Empty token means "leave the stored token unchanged" (see main process).
      apiToken: form.apiToken,
      jql: form.jql,
      pollIntervalMinutes: Number(form.pollIntervalMinutes) || 0,
      statusMapping,
    });
    setHasToken(hasToken || form.apiToken.length > 0);
    setForm((f) => ({ ...f, apiToken: "" }));
    setSaveMessage(t(($) => $.jira.saved));
  };

  return (
    <Card>
      <CardContent className="space-y-4 p-4">
        <div className="space-y-2">
          <Label htmlFor="jira-site">{t(($) => $.jira.site_url_label)}</Label>
          <Input
            id="jira-site"
            placeholder={t(($) => $.jira.site_url_placeholder)}
            value={form.siteUrl}
            onChange={(e) => setForm((f) => ({ ...f, siteUrl: e.target.value }))}
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="jira-email">{t(($) => $.jira.email_label)}</Label>
          <Input
            id="jira-email"
            type="email"
            value={form.email}
            onChange={(e) => setForm((f) => ({ ...f, email: e.target.value }))}
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="jira-token">{t(($) => $.jira.token_label)}</Label>
          <Input
            id="jira-token"
            type="password"
            placeholder={
              hasToken
                ? t(($) => $.jira.token_placeholder_stored)
                : t(($) => $.jira.token_placeholder_empty)
            }
            value={form.apiToken}
            onChange={(e) => setForm((f) => ({ ...f, apiToken: e.target.value }))}
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="jira-jql">{t(($) => $.jira.jql_label)}</Label>
          <Textarea
            id="jira-jql"
            rows={2}
            value={form.jql}
            onChange={(e) => setForm((f) => ({ ...f, jql: e.target.value }))}
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="jira-interval">{t(($) => $.jira.interval_label)}</Label>
          <Input
            id="jira-interval"
            type="number"
            min={0}
            value={form.pollIntervalMinutes}
            onChange={(e) =>
              setForm((f) => ({ ...f, pollIntervalMinutes: Number(e.target.value) || 0 }))
            }
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="jira-status-map">{t(($) => $.jira.status_map_label)}</Label>
          <Textarea
            id="jira-status-map"
            rows={4}
            placeholder={t(($) => $.jira.status_map_placeholder)}
            value={form.statusMappingText}
            onChange={(e) => setForm((f) => ({ ...f, statusMappingText: e.target.value }))}
          />
          <p className="text-xs text-muted-foreground">{t(($) => $.jira.status_map_hint)}</p>
        </div>

        <div className="flex items-center gap-3">
          <Button variant="outline" onClick={() => void onSave()}>
            {t(($) => $.jira.save)}
          </Button>
          <Button onClick={() => void syncNow()} disabled={running}>
            {running ? t(($) => $.jira.syncing) : t(($) => $.jira.sync_now)}
          </Button>
        </div>

        {saveMessage && <p className="text-sm text-muted-foreground">{saveMessage}</p>}
        {error && <p className="text-sm text-destructive">{error}</p>}
        {lastResult && (
          <p className="text-sm text-muted-foreground">
            {lastResult.errors.length > 0
              ? t(($) => $.jira.last_sync_with_errors, {
                  created: lastResult.created,
                  updated: lastResult.updated,
                  comments: lastResult.commentsAdded,
                  errors: lastResult.errors.length,
                })
              : t(($) => $.jira.last_sync, {
                  created: lastResult.created,
                  updated: lastResult.updated,
                  comments: lastResult.commentsAdded,
                })}
          </p>
        )}
      </CardContent>
    </Card>
  );
}
