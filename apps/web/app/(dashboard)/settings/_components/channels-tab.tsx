"use client";

import { useEffect, useState, useCallback } from "react";
import { Trash2, Hash, ExternalLink, Info, Pencil, X } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { toast } from "sonner";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { api } from "@/shared/api";
import type { Channel } from "@/shared/types";

const PROVIDERS = [
  { value: "slack", label: "Slack", icon: "#" },
] as const;

type ProviderType = (typeof PROVIDERS)[number]["value"];

function SlackGuideLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex items-center gap-0.5 text-primary hover:underline"
    >
      {children}
      <ExternalLink className="h-3 w-3" />
    </a>
  );
}

function FieldLabel({ label, tooltip }: { label: string; tooltip: string }) {
  return (
    <TooltipProvider>
      <div className="flex items-center gap-1">
        <label className="text-xs font-medium text-foreground">{label}</label>
        <Tooltip>
          <TooltipTrigger>
            <Info className="h-3 w-3 text-muted-foreground cursor-help" />
          </TooltipTrigger>
          <TooltipContent side="right" className="max-w-xs text-xs">
            {tooltip}
          </TooltipContent>
        </Tooltip>
      </div>
    </TooltipProvider>
  );
}

function SlackSetupGuide() {
  return (
    <div className="rounded-md border border-border bg-muted/50 p-3 space-y-2">
      <p className="text-xs font-medium">Setup Guide</p>
      <ol className="text-xs text-muted-foreground space-y-1.5 list-decimal list-inside">
        <li>
          <SlackGuideLink href="https://api.slack.com/apps">
            Create a Slack App
          </SlackGuideLink>
          {" "}in your workspace
        </li>
        <li>
          Go to <strong>OAuth & Permissions</strong> and add scopes:{" "}
          <code className="text-[10px] bg-muted px-1 py-0.5 rounded">chat:write</code>{" "}
          <code className="text-[10px] bg-muted px-1 py-0.5 rounded">channels:history</code>{" "}
          <code className="text-[10px] bg-muted px-1 py-0.5 rounded">channels:read</code>
        </li>
        <li>Install the app and copy the <strong>Bot User OAuth Token</strong></li>
        <li>
          Invite the bot to your channel:{" "}
          <code className="text-[10px] bg-muted px-1 py-0.5 rounded">/invite @your-bot</code>
        </li>
        <li>
          Get the Channel ID: right-click channel name &rarr; <strong>View channel details</strong> &rarr; copy ID at bottom
        </li>
      </ol>
    </div>
  );
}

function SlackFields({
  name, setName, botToken, setBotToken, channelId, setChannelId,
}: {
  name: string; setName: (v: string) => void;
  botToken: string; setBotToken: (v: string) => void;
  channelId: string; setChannelId: (v: string) => void;
}) {
  return (
    <>
      <div className="space-y-1.5">
        <label className="text-xs font-medium text-foreground">Display Name</label>
        <Input placeholder="e.g. #dev-agents" value={name} onChange={(e) => setName(e.target.value)} className="text-sm" />
      </div>
      <div className="space-y-1.5">
        <FieldLabel label="Bot Token" tooltip="Found in your Slack App > OAuth & Permissions > Bot User OAuth Token. Starts with xoxb-" />
        <Input type="password" placeholder="xoxb-..." value={botToken} onChange={(e) => setBotToken(e.target.value)} className="text-sm font-mono" />
      </div>
      <div className="space-y-1.5">
        <FieldLabel label="Channel ID" tooltip="Right-click the channel name in Slack > View channel details > Copy the ID at the bottom (starts with C)" />
        <Input placeholder="C0123456789" value={channelId} onChange={(e) => setChannelId(e.target.value)} className="text-sm font-mono" />
      </div>
    </>
  );
}

export function ChannelsTab() {
  const user = useAuthStore((s) => s.user);
  const members = useWorkspaceStore((s) => s.members);
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  const [channels, setChannels] = useState<Channel[]>([]);

  // Create form state
  const [provider, setProvider] = useState<ProviderType | "">("");
  const [name, setName] = useState("");
  const [botToken, setBotToken] = useState("");
  const [channelId, setChannelId] = useState("");
  const [creating, setCreating] = useState(false);

  // Edit state
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editName, setEditName] = useState("");
  const [editBotToken, setEditBotToken] = useState("");
  const [editChannelId, setEditChannelId] = useState("");
  const [saving, setSaving] = useState(false);

  const loadChannels = useCallback(async () => {
    try {
      const list = await api.listChannels();
      setChannels(list);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to load channels");
    }
  }, []);

  useEffect(() => { loadChannels(); }, [loadChannels]);

  const resetCreateForm = () => {
    setProvider("");
    setName("");
    setBotToken("");
    setChannelId("");
  };

  const handleCreate = async () => {
    if (provider !== "slack" || !name.trim() || !botToken.trim() || !channelId.trim()) return;
    setCreating(true);
    try {
      await api.createChannel({
        name: name.trim(),
        provider: "slack",
        config: { bot_token: botToken.trim(), channel_id: channelId.trim() },
      });
      resetCreateForm();
      await loadChannels();
      toast.success("Channel connected");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to create channel");
    } finally {
      setCreating(false);
    }
  };

  const handleStartEdit = (ch: Channel) => {
    setEditingId(ch.id);
    setEditName(ch.name);
    setEditBotToken("");
    setEditChannelId("");
  };

  const handleCancelEdit = () => {
    setEditingId(null);
  };

  const handleSaveEdit = async () => {
    if (!editingId || !editName.trim()) return;
    setSaving(true);
    try {
      const update: { name?: string; config?: Record<string, string> } = { name: editName.trim() };
      if (editBotToken.trim() || editChannelId.trim()) {
        update.config = {};
        if (editBotToken.trim()) update.config.bot_token = editBotToken.trim();
        if (editChannelId.trim()) update.config.channel_id = editChannelId.trim();
      }
      await api.updateChannel(editingId, update);
      setEditingId(null);
      await loadChannels();
      toast.success("Channel updated");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update channel");
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await api.deleteChannel(id);
      setChannels((prev) => prev.filter((c) => c.id !== id));
      if (editingId === id) setEditingId(null);
      toast.success("Channel removed");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to delete channel");
    }
  };

  const isCreateValid = provider === "slack" && name.trim() && botToken.trim() && channelId.trim();

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-semibold">Channels</h2>
          <p className="text-xs text-muted-foreground mt-1">
            Connect messaging channels so agents can ask questions and interact with your team during task execution.
          </p>
        </div>

        {/* Create form */}
        {canManage && (
          <Card>
            <CardContent className="space-y-4">
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-foreground">Provider</label>
                <Select value={provider} onValueChange={(v) => { if (v) setProvider(v as ProviderType); }}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="Select a provider..." />
                  </SelectTrigger>
                  <SelectContent>
                    {PROVIDERS.map((p) => (
                      <SelectItem key={p.value} value={p.value}>
                        {p.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              {provider === "slack" && (
                <div className="space-y-3 pt-1">
                  <SlackSetupGuide />
                  <SlackFields
                    name={name} setName={setName}
                    botToken={botToken} setBotToken={setBotToken}
                    channelId={channelId} setChannelId={setChannelId}
                  />
                  <div className="flex gap-2 pt-1">
                    <Button onClick={handleCreate} disabled={creating || !isCreateValid} size="sm">
                      {creating ? "Connecting..." : "Connect Channel"}
                    </Button>
                    <Button variant="ghost" size="sm" onClick={resetCreateForm}>
                      Cancel
                    </Button>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        )}

        {/* Channel list */}
        {channels.length === 0 && !canManage && (
          <p className="text-sm text-muted-foreground">No channels configured yet.</p>
        )}

        {channels.map((ch) => (
          <Card key={ch.id}>
            <CardContent className="py-3">
              {editingId === ch.id ? (
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-medium">Edit Channel</span>
                    <Button variant="ghost" size="icon-sm" onClick={handleCancelEdit}>
                      <X className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                  <div className="space-y-1.5">
                    <label className="text-xs font-medium text-foreground">Display Name</label>
                    <Input value={editName} onChange={(e) => setEditName(e.target.value)} className="text-sm" />
                  </div>
                  <div className="space-y-1.5">
                    <FieldLabel label="Bot Token" tooltip="Leave empty to keep current token" />
                    <Input type="password" placeholder="Leave empty to keep current" value={editBotToken} onChange={(e) => setEditBotToken(e.target.value)} className="text-sm font-mono" />
                  </div>
                  <div className="space-y-1.5">
                    <FieldLabel label="Channel ID" tooltip="Leave empty to keep current" />
                    <Input placeholder="Leave empty to keep current" value={editChannelId} onChange={(e) => setEditChannelId(e.target.value)} className="text-sm font-mono" />
                  </div>
                  <div className="flex gap-2">
                    <Button onClick={handleSaveEdit} disabled={saving || !editName.trim()} size="sm">
                      {saving ? "Saving..." : "Save"}
                    </Button>
                    <Button variant="ghost" size="sm" onClick={handleCancelEdit}>Cancel</Button>
                  </div>
                </div>
              ) : (
                <div className="flex items-center gap-3">
                  <div className="flex h-8 w-8 items-center justify-center rounded-md bg-muted">
                    <Hash className="h-4 w-4 text-muted-foreground" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-medium truncate">{ch.name}</div>
                    <div className="text-xs text-muted-foreground">
                      {ch.provider} &middot; Added {new Date(ch.created_at).toLocaleDateString()}
                    </div>
                  </div>
                  {canManage && (
                    <div className="flex gap-1">
                      <Button variant="ghost" size="icon-sm" onClick={() => handleStartEdit(ch)}>
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                      <Button variant="ghost" size="icon-sm" onClick={() => handleDelete(ch.id)}>
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  )}
                </div>
              )}
            </CardContent>
          </Card>
        ))}
      </section>
    </div>
  );
}
