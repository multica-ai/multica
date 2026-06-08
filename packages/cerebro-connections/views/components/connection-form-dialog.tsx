"use client";

import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { toast } from "sonner";

import type { Connection, ConnectionType, CreateConnectionInput, UpdateConnectionInput } from "../types";
import { useCreateConnection, useUpdateConnection } from "../queries";

interface Props {
  wsId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  existing?: Connection;
}

type AuthType = "none" | "bearer" | "apikey";

function detectAuthType(auth: Connection["auth_config"]): AuthType {
  if (auth.bearer_token) return "bearer";
  if (auth.api_key) return "apikey";
  return "none";
}

const EMPTY_FORM = {
  name: "",
  display_name: "",
  type: "mcp_http" as ConnectionType,
  url: "",
  internal: false,
  bearer_token: "",
  api_key: "",
  api_key_header: "",
  cf_access_id: "",
  cf_access_secret: "",
  enabled: true,
};

export function ConnectionFormDialog({ wsId, open, onOpenChange, existing }: Props) {
  const isEdit = !!existing;
  const create = useCreateConnection(wsId);
  const update = useUpdateConnection(wsId, existing?.id ?? "");

  const [form, setForm] = useState(() =>
    existing
      ? {
          name: existing.name,
          display_name: existing.display_name,
          type: existing.type as ConnectionType,
          url: existing.url,
          internal: existing.internal,
          bearer_token: existing.auth_config.bearer_token ?? "",
          api_key: existing.auth_config.api_key ?? "",
          api_key_header: existing.auth_config.api_key_header ?? "",
          cf_access_id: existing.auth_config.cf_access_id ?? "",
          cf_access_secret: existing.auth_config.cf_access_secret ?? "",
          enabled: existing.enabled,
        }
      : { ...EMPTY_FORM },
  );

  const [authType, setAuthType] = useState<AuthType>(() =>
    existing ? detectAuthType(existing.auth_config) : "none",
  );

  function field(k: keyof typeof form) {
    return (e: React.ChangeEvent<HTMLInputElement>) =>
      setForm((f) => ({ ...f, [k]: e.target.value }));
  }

  function handleAuthTypeChange(v: AuthType) {
    setAuthType(v);
    // Clear the other auth fields when switching type
    if (v !== "bearer") setForm((f) => ({ ...f, bearer_token: "" }));
    if (v !== "apikey") setForm((f) => ({ ...f, api_key: "", api_key_header: "" }));
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const auth_config = {
      ...(authType === "bearer" && form.bearer_token ? { bearer_token: form.bearer_token } : {}),
      ...(authType === "apikey" && form.api_key
        ? { api_key: form.api_key, ...(form.api_key_header ? { api_key_header: form.api_key_header } : {}) }
        : {}),
      ...(form.cf_access_id ? { cf_access_id: form.cf_access_id } : {}),
      ...(form.cf_access_secret ? { cf_access_secret: form.cf_access_secret } : {}),
    };
    try {
      if (isEdit) {
        const input: UpdateConnectionInput = {
          display_name: form.display_name,
          url: form.url,
          internal: form.internal,
          auth_config,
          endpoint_permissions: existing!.endpoint_permissions,
          enabled: form.enabled,
        };
        await update.mutateAsync(input);
        toast.success("Connection updated.");
      } else {
        const input: CreateConnectionInput = {
          name: form.name,
          display_name: form.display_name,
          type: form.type,
          url: form.url,
          internal: form.internal,
          auth_config,
          endpoint_permissions: [],
        };
        await create.mutateAsync(input);
        toast.success("Connection created.");
      }
      onOpenChange(false);
    } catch {
      toast.error("Something went wrong. Please try again.");
    }
  }

  const isPending = create.isPending || update.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit connection" : "New connection"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={(e) => void handleSubmit(e)} className="space-y-4">
          {!isEdit && (
            <div className="space-y-1">
              <Label htmlFor="conn-name">
                Name <span className="text-muted-foreground text-xs">(cannot change after creation)</span>
              </Label>
              <Input
                id="conn-name"
                placeholder="customer-service-mcp"
                value={form.name}
                onChange={field("name")}
                pattern="[a-z0-9_\-]{1,64}"
                required
              />
              <p className="text-xs text-muted-foreground">
                Lowercase letters, numbers, hyphens and underscores only. Max 64 characters.
              </p>
            </div>
          )}

          <div className="space-y-1">
            <Label htmlFor="conn-display-name">Display name</Label>
            <Input
              id="conn-display-name"
              placeholder="Customer Service MCP"
              value={form.display_name}
              onChange={field("display_name")}
              required
            />
          </div>

          {!isEdit && (
            <div className="space-y-1">
              <Label htmlFor="conn-type">Type</Label>
              <Select
                value={form.type}
                onValueChange={(v) => setForm((f) => ({ ...f, type: v as ConnectionType }))}
              >
                <SelectTrigger id="conn-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="mcp_http">MCP (HTTP)</SelectItem>
                  <SelectItem value="api">REST API</SelectItem>
                </SelectContent>
              </Select>
            </div>
          )}

          <div className="space-y-1">
            <Label htmlFor="conn-url">URL</Label>
            <Input
              id="conn-url"
              placeholder="http://customer-service-mcp.internal:3000/mcp"
              value={form.url}
              onChange={field("url")}
              required
            />
          </div>

          <div className="flex items-center gap-2">
            <Switch
              id="conn-internal"
              checked={form.internal}
              onCheckedChange={(v) => setForm((f) => ({ ...f, internal: v }))}
            />
            <Label htmlFor="conn-internal">
              Internal Sliplane path{" "}
              <span className="text-muted-foreground text-xs">
                (*.internal — not reachable from outside)
              </span>
            </Label>
          </div>

          {/* Authentication — pick one type */}
          <div className="space-y-3 rounded-md border p-3">
            <div className="space-y-1">
              <Label htmlFor="conn-auth-type">Authentication</Label>
              <Select value={authType} onValueChange={(v) => handleAuthTypeChange(v as AuthType)}>
                <SelectTrigger id="conn-auth-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">None</SelectItem>
                  <SelectItem value="bearer">Bearer token</SelectItem>
                  <SelectItem value="apikey">API key</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {authType === "bearer" && (
              <div className="space-y-1">
                <Label htmlFor="conn-bearer">Bearer token</Label>
                <Input
                  id="conn-bearer"
                  type="password"
                  placeholder={isEdit ? "Unchanged (*** = saved)" : ""}
                  value={form.bearer_token}
                  onChange={field("bearer_token")}
                  autoComplete="off"
                />
              </div>
            )}

            {authType === "apikey" && (
              <div className="grid grid-cols-2 gap-2">
                <div className="space-y-1">
                  <Label htmlFor="conn-apikey">API key</Label>
                  <Input
                    id="conn-apikey"
                    type="password"
                    placeholder={isEdit ? "Unchanged" : ""}
                    value={form.api_key}
                    onChange={field("api_key")}
                    autoComplete="off"
                  />
                </div>
                <div className="space-y-1">
                  <Label htmlFor="conn-apikey-header">Header name</Label>
                  <Input
                    id="conn-apikey-header"
                    placeholder="X-API-Key"
                    value={form.api_key_header}
                    onChange={field("api_key_header")}
                  />
                </div>
              </div>
            )}
          </div>

          {/* Cloudflare Access — separate from auth, added when the endpoint is
              behind Cloudflare Access (network layer, not API auth) */}
          <div className="space-y-3 rounded-md border p-3">
            <p className="text-sm font-medium">Cloudflare Access <span className="text-xs font-normal text-muted-foreground">(optional — only for CF-protected endpoints)</span></p>
            <div className="grid grid-cols-2 gap-2">
              <div className="space-y-1">
                <Label htmlFor="conn-cf-id">Client ID</Label>
                <Input
                  id="conn-cf-id"
                  value={form.cf_access_id}
                  onChange={field("cf_access_id")}
                  autoComplete="off"
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="conn-cf-secret">Client Secret</Label>
                <Input
                  id="conn-cf-secret"
                  type="password"
                  value={form.cf_access_secret}
                  onChange={field("cf_access_secret")}
                  autoComplete="off"
                />
              </div>
            </div>
          </div>

          {isEdit && (
            <div className="flex items-center gap-2">
              <Switch
                id="conn-enabled"
                checked={form.enabled}
                onCheckedChange={(v) => setForm((f) => ({ ...f, enabled: v }))}
              />
              <Label htmlFor="conn-enabled">Active</Label>
            </div>
          )}

          <div className="flex justify-end gap-2 pt-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? "Saving…" : isEdit ? "Save changes" : "Create connection"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
