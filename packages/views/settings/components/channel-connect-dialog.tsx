"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent } from "@multica/core/types";
import { api, errorCode } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";
import { docsLocalePrefix } from "../../common/docs-locale";
import { openExternal } from "../../platform";
import { IntegrationChannelIcon } from "./integration-channel-icon";
import {
  CHANNEL_INTEGRATIONS,
  type ChannelIntegration,
} from "./settings-navigation";
import { useMessagingChannels } from "./use-messaging-channels";
import { LarkChannelAuthorization } from "./lark-channel-authorization";
import { WecomChannelAuthorization } from "./wecom-channel-authorization";

export function ChannelConnectDialog({
  agents,
  initialAgent,
  initialChannel,
  open,
  onOpenChange,
}: {
  agents: Agent[];
  initialAgent?: Agent;
  initialChannel?: ChannelIntegration;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("settings");
  const [busy, setBusy] = useState(false);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!busy) onOpenChange(next);
      }}
    >
      <DialogContent className="sm:max-w-lg" showCloseButton={!busy}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.channels.connect_title)}</DialogTitle>
        </DialogHeader>
        {open ? (
          <ChannelConnectSteps
            agents={agents}
            initialAgent={initialAgent}
            initialChannel={initialChannel}
            onClose={() => onOpenChange(false)}
            onBusyChange={setBusy}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function ChannelConnectSteps({
  agents,
  initialAgent,
  initialChannel,
  onClose,
  onBusyChange,
}: {
  agents: Agent[];
  initialAgent?: Agent;
  initialChannel?: ChannelIntegration;
  onClose: () => void;
  onBusyChange: (busy: boolean) => void;
}) {
  const { t, i18n } = useT("settings");
  const { wsId, queries, connections, isAdmin, canManage, available } =
    useMessagingChannels();
  const [agentId, setAgentId] = useState(initialAgent?.id ?? "");
  const [channel, setChannel] = useState<ChannelIntegration | undefined>(
    initialChannel,
  );
  const [authorizing, setAuthorizing] = useState(false);
  const [wecomMethod, setWecomMethod] = useState<"scan" | "credentials">(
    "scan",
  );
  const agent =
    initialAgent ?? agents.find((candidate) => candidate.id === agentId);
  const connected = (id: ChannelIntegration) =>
    connections.some(
      (entry) =>
        entry.channel === id && entry.installation.agent_id === agent?.id,
    );
  // Picking a platform is the only choice left once the agent is known, so the
  // row itself starts the connection instead of waiting for a Continue click.
  const connect = (id: ChannelIntegration) => {
    setChannel(id);
    setAuthorizing(true);
  };
  const scanWecom =
    channel === "wecom" &&
    queries.wecom.data?.scan_install_supported === true &&
    wecomMethod === "scan";

  return (
    <>
      <DialogDescription>
        {authorizing && agent && channel
          ? t(($) => $.channels.authorize_description, {
              agent: agent.name,
              platform:
                channel === "lark"
                  ? t(($) => $.lark.region_feishu)
                  : t(($) => $[channel].section_title),
            })
          : t(($) => $.channels.choose_description)}
      </DialogDescription>
      {!authorizing ? (
        <>
          <div className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="channel-agent">
                {t(($) => $.channels.agent_label)}
              </Label>
              {initialAgent ? (
                <p className="text-body">{initialAgent.name}</p>
              ) : (
                <select
                  id="channel-agent"
                  className="h-9 w-full rounded-md border bg-background px-3 text-body"
                  value={agentId}
                  onChange={(event) => setAgentId(event.target.value)}
                >
                  <option value="">{t(($) => $.channels.choose_agent)}</option>
                  {agents
                    .filter(
                      (candidate) =>
                        !candidate.archived_at &&
                        (isAdmin ||
                          CHANNEL_INTEGRATIONS.some((id) =>
                            canManage(id, candidate),
                          )),
                    )
                    .map((candidate) => (
                      <option key={candidate.id} value={candidate.id}>
                        {candidate.name}
                      </option>
                    ))}
                </select>
              )}
            </div>
            <div
              className="space-y-1.5"
              role="group"
              aria-label={t(($) => $.channels.platform_label)}
            >
              <Label>{t(($) => $.channels.platform_label)}</Label>
              {CHANNEL_INTEGRATIONS.filter(
                (id) => queries[id].data?.configured || isAdmin,
              )
                .filter((id) => !connected(id))
                .map((id) => {
                  const permitted = !!agent && canManage(id, agent);
                  return (
                    <Button
                      key={id}
                      variant={channel === id ? "secondary" : "outline"}
                      className="w-full justify-start"
                      disabled={
                        !agent ||
                        !!agent.archived_at ||
                        !available(id) ||
                        !permitted
                      }
                      onClick={() => connect(id)}
                    >
                      <IntegrationChannelIcon channel={id} />
                      <span>
                        {id === "lark"
                          ? t(($) => $.lark.region_feishu)
                          : t(($) => $[id].section_title)}
                      </span>{" "}
                      <span className="ml-auto text-caption text-muted-foreground">
                        {queries[id].isError
                          ? t(($) => $.integrations.status_unknown)
                          : !available(id)
                            ? t(($) => $.channels.unavailable)
                            : agent && !permitted
                              ? t(($) => $.channels.ask_admin)
                              : null}
                      </span>
                    </Button>
                  );
                })}
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={onClose}>
              {t(($) => $.lark.install_close)}
            </Button>
          </DialogFooter>
        </>
      ) : !agent ||
        !channel ||
        !canManage(channel, agent) ||
        !available(channel) ? (
        <>
          <p role="alert" className="text-body text-muted-foreground">
            {t(($) => $.channels.connection_changed)}
          </p>
          <DialogFooter>
            <Button variant="outline" onClick={onClose}>
              {t(($) => $.lark.install_close)}
            </Button>
          </DialogFooter>
        </>
      ) : (
        <>
          <Button
            variant="link"
            className="w-fit p-0"
            onClick={() =>
              openExternal(
                `https://multica.ai/docs${docsLocalePrefix(i18n.language)}/${channel === "lark" ? "lark" : channel}-bot-integration`,
              )
            }
          >
            {t(($) => $.channels.setup_guide)}
          </Button>
          {channel === "wecom" && queries.wecom.data?.scan_install_supported ? (
            <div className="space-y-1.5">
              <Label htmlFor="channel-method">
                {t(($) => $.channels.method_label)}
              </Label>
              <select
                id="channel-method"
                value={wecomMethod}
                onChange={(event) =>
                  setWecomMethod(
                    event.target.value === "scan" ? "scan" : "credentials",
                  )
                }
                className="h-9 w-full rounded-md border bg-background px-3 text-body"
              >
                <option value="scan">
                  {t(($) => $.wecom.scan_option_title)}
                </option>
                <option value="credentials">
                  {t(($) => $.wecom.byo_dialog_title)}
                </option>
              </select>
            </div>
          ) : null}
          {channel === "lark" ? (
            <LarkChannelAuthorization
              wsId={wsId}
              agentId={agent.id}
              // International installs remain disabled until the persistence
              // issue is fixed; existing Lark bots stay manageable.
              region="feishu"
              onClose={onClose}
            />
          ) : scanWecom ? (
            <WecomChannelAuthorization
              wsId={wsId}
              agentId={agent.id}
              onClose={onClose}
            />
          ) : (
            <ChannelCredentials
              channel={channel}
              wsId={wsId}
              agentId={agent.id}
              onClose={onClose}
              onBusyChange={onBusyChange}
            />
          )}
        </>
      )}
    </>
  );
}

function ChannelCredentials({
  channel,
  wsId,
  agentId,
  onClose,
  onBusyChange,
}: {
  channel: Exclude<ChannelIntegration, "lark">;
  wsId: string;
  agentId: string;
  onClose: () => void;
  onBusyChange: (busy: boolean) => void;
}) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const [values, setValues] = useState({
    token: "",
    appToken: "",
    clientId: "",
    clientSecret: "",
    botId: "",
    secret: "",
    botName: "",
  });
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fields: {
    key: keyof typeof values;
    label: string;
    secret?: boolean;
    optional?: boolean;
  }[] =
    channel === "slack"
      ? [
          {
            key: "token",
            label: t(($) => $.slack.byo_bot_token_label),
            secret: true,
          },
          {
            key: "appToken",
            label: t(($) => $.slack.byo_app_token_label),
            secret: true,
          },
        ]
      : channel === "dingtalk"
        ? [
            {
              key: "clientId",
              label: t(($) => $.dingtalk.byo_appkey_label),
              secret: true,
            },
            {
              key: "clientSecret",
              label: t(($) => $.dingtalk.byo_appsecret_label),
              secret: true,
            },
          ]
        : channel === "wecom"
          ? [
              { key: "botId", label: t(($) => $.wecom.byo_bot_id_label) },
              {
                key: "secret",
                label: t(($) => $.wecom.byo_secret_label),
                secret: true,
              },
              {
                key: "botName",
                label: t(($) => $.wecom.byo_bot_name_label),
                optional: true,
              },
            ]
          : [
              {
                key: "token",
                label: t(($) => $.telegram.bot_token_label),
                secret: true,
              },
            ];

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (submitting) return;
    setSubmitting(true);
    onBusyChange(true);
    setError(null);
    try {
      const v = Object.fromEntries(
        Object.entries(values).map(([key, value]) => [key, value.trim()]),
      ) as typeof values;
      const installation =
        channel === "slack"
          ? await api.registerSlackBYO(wsId, agentId, {
              bot_token: v.token,
              app_token: v.appToken,
            })
          : channel === "dingtalk"
            ? await api.registerDingTalkBYO(wsId, agentId, {
                client_id: v.clientId,
                client_secret: v.clientSecret,
              })
            : channel === "wecom"
              ? await api.registerWecomBYO(wsId, agentId, {
                  bot_id: v.botId,
                  secret: v.secret,
                  bot_name: v.botName || undefined,
                })
              : await api.registerTelegramBot(wsId, agentId, {
                  bot_token: v.token,
                });
      if (!installation.id || installation.status !== "active")
        throw new Error(t(($) => $.channels.connect_failed));
      await qc.invalidateQueries({
        queryKey: [channel, wsId, "installations"],
      });
      toast.success(t(($) => $.channels.connect_success));
      onClose();
    } catch (reason) {
      const code = errorCode(reason);
      const wecomErrors: Record<string, string> = {
        wecom_bot_owned_by_same_workspace: t(
          ($) => $.wecom.byo_conflict_same_workspace,
        ),
        wecom_bot_owned_by_archived_agent: t(
          ($) => $.wecom.byo_conflict_archived_agent,
        ),
        wecom_bot_owned_by_another_workspace: t(
          ($) => $.wecom.byo_conflict_other_workspace,
        ),
        wecom_install_rejected: t(($) => $.wecom.byo_rejected),
        wecom_credentials_rejected: t(($) => $.wecom.byo_credentials_rejected),
        wecom_credentials_unverifiable: t(
          ($) => $.wecom.byo_credentials_unverifiable,
        ),
        wecom_install_failed: t(($) => $.wecom.byo_install_failed),
      };
      setError(
        (code && wecomErrors[code]) ||
          (reason instanceof Error
            ? reason.message
            : t(($) => $.channels.connect_failed)),
      );
    } finally {
      setSubmitting(false);
      onBusyChange(false);
    }
  };

  return (
    <form onSubmit={submit} className="grid gap-4" aria-busy={submitting}>
      {fields.map((field) => (
        <div key={field.key} className="space-y-1.5">
          <Label htmlFor={`channel-${field.key}`}>{field.label}</Label>
          <Input
            id={`channel-${field.key}`}
            type={field.secret ? "password" : "text"}
            autoComplete="off"
            spellCheck={false}
            required={!field.optional}
            disabled={submitting}
            value={values[field.key]}
            onChange={(event) =>
              setValues((old) => ({ ...old, [field.key]: event.target.value }))
            }
          />
        </div>
      ))}
      {error ? (
        <p role="alert" className="text-caption text-destructive">
          {error}
        </p>
      ) : null}
      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          disabled={submitting}
          onClick={onClose}
        >
          {t(($) => $.lark.install_close)}
        </Button>
        <Button
          type="submit"
          disabled={
            submitting ||
            fields.some((field) => !field.optional && !values[field.key].trim())
          }
        >
          {submitting
            ? t(($) => $.integrations.status_loading)
            : t(($) => $.channels.connect)}
        </Button>
      </DialogFooter>
    </form>
  );
}
