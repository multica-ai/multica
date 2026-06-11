"use client";

import { useState } from "react";
import {
  CheckCircle2,
  ChevronRight,
  Loader2,
  Plus,
  Settings,
  Trash2,
  XCircle,
} from "lucide-react";
import { toast } from "sonner";

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
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Badge } from "@multica/ui/components/ui/badge";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink, useNavigation } from "@multica/views/navigation";
import { PageHeader } from "@multica/views/layout/page-header";

import type { Connection, ConnectionType, CreateConnectionInput, EndpointPermission, UpdateConnectionInput } from "../types";
import {
  useConnection,
  useCreateConnection,
  useUpdateConnection,
  useTestConnection,
  type TestResult,
} from "../queries";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

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

// HTTP methods an admin can allow per endpoint. Order matches CRUD intuition.
const HTTP_METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"] as const;

// mergeEndpoints folds newly discovered endpoints into the admin's current list:
// a path that's already present keeps its curated methods unioned with the
// discovered ones; a new path is added with everything the spec declared. Sorted
// by path so the list stays stable across discoveries.
function mergeEndpoints(
  current: EndpointPermission[],
  discovered: { path: string; methods: string[] }[],
): EndpointPermission[] {
  const byPath = new Map<string, Set<string>>();
  for (const ep of current) byPath.set(ep.path, new Set(ep.methods));
  for (const ep of discovered) {
    const set = byPath.get(ep.path) ?? new Set<string>();
    for (const m of ep.methods) set.add(m.toUpperCase());
    byPath.set(ep.path, set);
  }
  return Array.from(byPath.entries())
    .map(([path, methods]) => ({ path, methods: Array.from(methods).sort() }))
    .sort((a, b) => a.path.localeCompare(b.path));
}

// ---------------------------------------------------------------------------
// Shared form body
// ---------------------------------------------------------------------------

interface FormBodyProps {
  mode: "create" | "edit";
  initial: typeof EMPTY_FORM;
  initialAuthType: AuthType;
  initialEndpoints?: EndpointPermission[];
  existingConn?: Connection;
  onSave: (form: typeof EMPTY_FORM, authType: AuthType, endpoints: EndpointPermission[]) => Promise<void>;
  isSaving: boolean;
  // Which secret fields had a value on load (server masks them as "***")
  secretsSet?: { bearerToken: boolean; apiKey: boolean; cfSecret: boolean };
}

function ConnectionFormBody({
  mode,
  initial,
  initialAuthType,
  initialEndpoints,
  existingConn,
  onSave,
  isSaving,
  secretsSet,
}: FormBodyProps) {
  const wsId = useWorkspaceId();
  const router = useNavigation();
  const wsPaths = useWorkspacePaths();

  const [form, setForm] = useState(initial);
  const [authType, setAuthType] = useState<AuthType>(initialAuthType);
  const [endpoints, setEndpoints] = useState<EndpointPermission[]>(initialEndpoints ?? []);
  const [testResult, setTestResult] = useState<TestResult | null>(null);

  const testConn = useTestConnection(wsId ?? "");

  const isCreate = mode === "create";
  const isEdit = !isCreate;

  function field(k: keyof typeof form) {
    return (e: React.ChangeEvent<HTMLInputElement>) =>
      setForm((f) => ({ ...f, [k]: e.target.value }));
  }

  function handleAuthTypeChange(v: AuthType) {
    setAuthType(v);
    if (v !== "bearer") setForm((f) => ({ ...f, bearer_token: "" }));
    if (v !== "apikey") setForm((f) => ({ ...f, api_key: "", api_key_header: "" }));
  }

  async function handleTest() {
    if (!form.url) {
      toast.error("Enter a URL first.");
      return;
    }
    setTestResult(null);
    const auth_config = buildAuthConfig(form, authType);
    const result = await testConn.mutateAsync({
      url: form.url,
      type: form.type,
      auth_config,
      // Pass the connection ID when editing so the backend can fill in masked credentials.
      ...(existingConn?.id ? { connection_id: existingConn.id } : {}),
    });
    setTestResult(result);
    // For API connections, discovery returns the endpoint catalogue from the
    // API's OpenAPI/Swagger spec — fold it into the editable list.
    if (form.type === "api" && result.endpoints && result.endpoints.length > 0) {
      setEndpoints((prev) => mergeEndpoints(prev, result.endpoints ?? []));
      toast.success(`Discovered ${result.endpoints.length} endpoint${result.endpoints.length !== 1 ? "s" : ""}.`);
    }
  }

  function addEndpoint() {
    setEndpoints((prev) => [...prev, { path: "", methods: ["GET"] }]);
  }

  function removeEndpoint(index: number) {
    setEndpoints((prev) => prev.filter((_, i) => i !== index));
  }

  function setEndpointPath(index: number, path: string) {
    setEndpoints((prev) => prev.map((ep, i) => (i === index ? { ...ep, path } : ep)));
  }

  function toggleEndpointMethod(index: number, method: string) {
    setEndpoints((prev) =>
      prev.map((ep, i) => {
        if (i !== index) return ep;
        const has = ep.methods.includes(method);
        const methods = has ? ep.methods.filter((m) => m !== method) : [...ep.methods, method];
        return { ...ep, methods };
      }),
    );
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    // Drop blank-path rows so an empty editor row doesn't persist as a junk endpoint.
    const cleaned = endpoints
      .map((ep) => ({ path: ep.path.trim(), methods: ep.methods }))
      .filter((ep) => ep.path !== "" && ep.methods.length > 0);
    await onSave(form, authType, cleaned);
  }

  const backPath = wsPaths.settings();

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2 text-xs min-w-0">
          <AppLink
            href={backPath}
            className="text-muted-foreground hover:text-foreground transition-colors shrink-0"
          >
            <Settings className="h-4 w-4" />
          </AppLink>
          <span className="text-muted-foreground shrink-0">/</span>
          <AppLink
            href={backPath}
            className="text-muted-foreground hover:text-foreground transition-colors shrink-0"
          >
            Connections
          </AppLink>
          <ChevronRight className="size-3 text-muted-foreground/40 shrink-0" />
          <span className="text-muted-foreground truncate">
            {isCreate ? "New connection" : (form.display_name || existingConn?.display_name || "Edit")}
          </span>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => router.push(backPath)}
            disabled={isSaving}
          >
            Cancel
          </Button>
          <Button
            size="sm"
            type="submit"
            form="conn-form"
            disabled={isSaving}
          >
            {isSaving ? (
              <>
                <Loader2 className="mr-1.5 size-3.5 animate-spin" />
                Saving…
              </>
            ) : isCreate ? "Create connection" : "Save changes"}
          </Button>
        </div>
      </PageHeader>

      {/* Body */}
      <div className="flex-1 min-h-0 overflow-y-auto">
        <form
          id="conn-form"
          onSubmit={(e) => void handleSubmit(e)}
          className="mx-auto max-w-2xl px-6 py-8 space-y-6"
        >
          {/* Name (new only) */}
          {isCreate && (
            <div className="space-y-1.5">
              <Label htmlFor="conn-name">
                Name{" "}
                <span className="text-xs text-muted-foreground font-normal">
                  (cannot change after creation)
                </span>
              </Label>
              <Input
                id="conn-name"
                placeholder="customer-service-mcp"
                value={form.name}
                onChange={field("name")}
                pattern="[a-z0-9_\-]{1,64}"
                required
                title="Lowercase letters, numbers, hyphens and underscores. Max 64 characters."
              />
              <p className="text-xs text-muted-foreground">
                Lowercase letters, numbers, hyphens and underscores. Max 64 characters.
              </p>
            </div>
          )}

          {/* Display name */}
          <div className="space-y-1.5">
            <Label htmlFor="conn-display-name">Display name</Label>
            <Input
              id="conn-display-name"
              placeholder="Customer Service MCP"
              value={form.display_name}
              onChange={field("display_name")}
              required
            />
          </div>

          {/* Type (new only) */}
          {isCreate && (
            <div className="space-y-1.5">
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

          {/* URL + Test */}
          <div className="space-y-1.5">
            <Label htmlFor="conn-url">URL</Label>
            <div className="flex gap-2">
              <Input
                id="conn-url"
                placeholder="http://customer-service-mcp.internal:3000/mcp"
                value={form.url}
                onChange={field("url")}
                required
                className="flex-1"
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => void handleTest()}
                disabled={testConn.isPending || !form.url}
                className="shrink-0"
              >
                {testConn.isPending ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : form.type === "api" ? (
                  "Test & discover"
                ) : (
                  "Test"
                )}
              </Button>
            </div>
          </div>

          {/* Test result */}
          {testResult && (
            <div
              className={`rounded-md border p-4 ${
                testResult.reachable
                  ? "border-green-200 bg-green-50 dark:border-green-900 dark:bg-green-950/30"
                  : "border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950/30"
              }`}
            >
              <div className="flex items-center gap-2 mb-2">
                {testResult.reachable ? (
                  <CheckCircle2 className="size-4 text-green-600 dark:text-green-400 shrink-0" />
                ) : (
                  <XCircle className="size-4 text-red-600 dark:text-red-400 shrink-0" />
                )}
                <span className="text-sm font-medium">
                  {testResult.reachable ? "Connection successful" : "Connection failed"}
                  {testResult.status_code ? ` — HTTP ${testResult.status_code}` : ""}
                </span>
              </div>

              {testResult.error && (
                <p className="text-xs text-muted-foreground mb-2">{testResult.error}</p>
              )}

              {testResult.tools && testResult.tools.length > 0 && (
                <div className="space-y-1">
                  <p className="text-xs font-medium text-muted-foreground">
                    {testResult.tools.length} tool{testResult.tools.length !== 1 ? "s" : ""} discovered:
                  </p>
                  <div className="flex flex-wrap gap-1.5">
                    {testResult.tools.map((t) => (
                      <Badge key={t.name} variant="secondary" className="text-xs font-mono">
                        {t.name}
                      </Badge>
                    ))}
                  </div>
                </div>
              )}

              {testResult.endpoints && testResult.endpoints.length > 0 && (
                <div className="space-y-1">
                  <p className="text-xs font-medium text-muted-foreground">
                    {testResult.endpoints.length} endpoint{testResult.endpoints.length !== 1 ? "s" : ""} discovered — added to the list below.
                  </p>
                </div>
              )}

              {testResult.reachable &&
                (!testResult.tools || testResult.tools.length === 0) &&
                (!testResult.endpoints || testResult.endpoints.length === 0) && (
                  <p className="text-xs text-muted-foreground">
                    {form.type === "mcp_http"
                      ? "Server reachable — no tools returned (server may require initialize handshake)."
                      : "Server reachable — no OpenAPI/Swagger spec found. Add endpoints manually below."}
                  </p>
                )}
            </div>
          )}

          {/* Internal toggle */}
          <div className="flex items-center gap-3">
            <Switch
              id="conn-internal"
              checked={form.internal}
              onCheckedChange={(v) => setForm((f) => ({ ...f, internal: v }))}
            />
            <div>
              <Label htmlFor="conn-internal">Internal Sliplane path</Label>
              <p className="text-xs text-muted-foreground">
                Only accessible within the cluster (*.internal URLs)
              </p>
            </div>
          </div>

          {/* Authentication */}
          <div className="space-y-3 rounded-md border p-4">
            <div className="space-y-1.5">
              <Label htmlFor="conn-auth-type">Authentication</Label>
              <Select
                value={authType}
                onValueChange={(v) => handleAuthTypeChange(v as AuthType)}
              >
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
              <div className="space-y-1.5">
                <div className="flex items-center gap-2">
                  <Label htmlFor="conn-bearer">Bearer token</Label>
                  {secretsSet?.bearerToken && !form.bearer_token && (
                    <Badge variant="secondary" className="text-xs font-normal">Set</Badge>
                  )}
                </div>
                <Input
                  id="conn-bearer"
                  type="password"
                  placeholder={secretsSet?.bearerToken && !form.bearer_token ? "•••••••• — type to replace" : ""}
                  value={form.bearer_token}
                  onChange={field("bearer_token")}
                  autoComplete="off"
                />
              </div>
            )}

            {authType === "apikey" && (
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1.5">
                  <div className="flex items-center gap-2">
                    <Label htmlFor="conn-apikey">API key</Label>
                    {secretsSet?.apiKey && !form.api_key && (
                      <Badge variant="secondary" className="text-xs font-normal">Set</Badge>
                    )}
                  </div>
                  <Input
                    id="conn-apikey"
                    type="password"
                    placeholder={secretsSet?.apiKey && !form.api_key ? "•••••••• — type to replace" : ""}
                    value={form.api_key}
                    onChange={field("api_key")}
                    autoComplete="off"
                  />
                </div>
                <div className="space-y-1.5">
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

          {/* Cloudflare Access */}
          <div className="space-y-3 rounded-md border p-4">
            <div>
              <p className="text-sm font-medium">Cloudflare Access</p>
              <p className="text-xs text-muted-foreground">
                Only needed for endpoints protected by Cloudflare Access (network layer, separate from API auth)
              </p>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label htmlFor="conn-cf-id">Client ID</Label>
                <Input
                  id="conn-cf-id"
                  value={form.cf_access_id}
                  onChange={field("cf_access_id")}
                  autoComplete="off"
                />
              </div>
              <div className="space-y-1.5">
                <div className="flex items-center gap-2">
                  <Label htmlFor="conn-cf-secret">Client Secret</Label>
                  {secretsSet?.cfSecret && !form.cf_access_secret && (
                    <Badge variant="secondary" className="text-xs font-normal">Set</Badge>
                  )}
                </div>
                <Input
                  id="conn-cf-secret"
                  type="password"
                  placeholder={secretsSet?.cfSecret && !form.cf_access_secret ? "•••••••• — type to replace" : ""}
                  value={form.cf_access_secret}
                  onChange={field("cf_access_secret")}
                  autoComplete="off"
                />
              </div>
            </div>
          </div>

          {/* Endpoints (REST API only) — the catalogue of paths agents may call,
              each gated per HTTP method. Populated by "Test & discover" from the
              API's OpenAPI/Swagger spec, or added by hand. */}
          {form.type === "api" && (
            <div className="space-y-3 rounded-md border p-4">
              <div className="flex items-start justify-between gap-2">
                <div>
                  <p className="text-sm font-medium">Endpoints</p>
                  <p className="text-xs text-muted-foreground">
                    The paths agents may call, gated per method. Use “Test &amp; discover” to read them
                    from the API’s spec, or add them by hand. Fine-grained allow/deny per level is set
                    afterwards under Permissions.
                  </p>
                </div>
                <Button type="button" variant="outline" size="sm" onClick={addEndpoint} className="shrink-0">
                  <Plus className="size-3.5 mr-1" />
                  Add
                </Button>
              </div>

              {endpoints.length === 0 ? (
                <p className="text-xs text-muted-foreground italic">
                  No endpoints yet. Click “Test &amp; discover” above, or “Add” to enter one manually.
                </p>
              ) : (
                <div className="space-y-2">
                  {endpoints.map((ep, i) => (
                    <div key={i} className="flex items-center gap-2">
                      <Input
                        value={ep.path}
                        onChange={(e) => setEndpointPath(i, e.target.value)}
                        placeholder="/orders/{id}"
                        className="flex-1 font-mono text-xs"
                      />
                      <div className="flex gap-1 shrink-0">
                        {HTTP_METHODS.map((m) => {
                          const active = ep.methods.includes(m);
                          return (
                            <Button
                              key={m}
                              type="button"
                              size="sm"
                              variant={active ? "default" : "outline"}
                              onClick={() => toggleEndpointMethod(i, m)}
                              className="h-7 px-2 text-[10px] font-mono"
                            >
                              {m}
                            </Button>
                          );
                        })}
                      </div>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => removeEndpoint(i)}
                        className="shrink-0 text-muted-foreground hover:text-destructive"
                        aria-label="Remove endpoint"
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Enabled (edit only) */}
          {isEdit && (
            <div className="flex items-center gap-3">
              <Switch
                id="conn-enabled"
                checked={form.enabled}
                onCheckedChange={(v) => setForm((f) => ({ ...f, enabled: v }))}
              />
              <Label htmlFor="conn-enabled">Active</Label>
            </div>
          )}
        </form>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Auth config builder
// ---------------------------------------------------------------------------

function buildAuthConfig(form: typeof EMPTY_FORM, authType: AuthType): Connection["auth_config"] {
  return {
    ...(authType === "bearer" && form.bearer_token ? { bearer_token: form.bearer_token } : {}),
    ...(authType === "apikey" && form.api_key
      ? { api_key: form.api_key, ...(form.api_key_header ? { api_key_header: form.api_key_header } : {}) }
      : {}),
    ...(form.cf_access_id ? { cf_access_id: form.cf_access_id } : {}),
    ...(form.cf_access_secret ? { cf_access_secret: form.cf_access_secret } : {}),
  };
}

// ---------------------------------------------------------------------------
// Public exports
// ---------------------------------------------------------------------------

export function ConnectionCreatePage() {
  const wsId = useWorkspaceId();
  const router = useNavigation();
  const wsPaths = useWorkspacePaths();
  const create = useCreateConnection(wsId ?? "");

  async function handleSave(form: typeof EMPTY_FORM, authType: AuthType, endpoints: EndpointPermission[]) {
    const input: CreateConnectionInput = {
      name: form.name,
      display_name: form.display_name,
      type: form.type,
      url: form.url,
      internal: form.internal,
      auth_config: buildAuthConfig(form, authType),
      endpoint_permissions: endpoints,
    };
    await create.mutateAsync(input);
    toast.success("Connection created.");
    router.push(wsPaths.settings());
  }

  return (
    <ConnectionFormBody
      mode="create"
      initial={{ ...EMPTY_FORM }}
      initialAuthType="none"
      initialEndpoints={[]}
      onSave={(form, authType, endpoints) => {
        return handleSave(form, authType, endpoints).catch(() => {
          toast.error("Something went wrong. Please try again.");
        });
      }}
      isSaving={create.isPending}
    />
  );
}

export function ConnectionEditPage({ connId }: { connId: string }) {
  const wsId = useWorkspaceId();
  const router = useNavigation();
  const wsPaths = useWorkspacePaths();
  const { data: conn, isLoading } = useConnection(wsId ?? "", connId);
  const update = useUpdateConnection(wsId ?? "", connId);

  if (isLoading) {
    return (
      <div className="flex h-full flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-5">
          <Skeleton className="h-4 w-4" />
          <span className="text-muted-foreground">/</span>
          <Skeleton className="h-4 w-32" />
        </div>
        <div className="mx-auto max-w-2xl px-6 py-8 space-y-4 w-full">
          <Skeleton className="h-6 w-48" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      </div>
    );
  }

  if (!conn || !conn.id) {
    return (
      <div className="flex h-full items-center justify-center text-muted-foreground">
        Connection not found.
      </div>
    );
  }

  const secretsSet = {
    bearerToken: conn.auth_config.bearer_token === "***",
    apiKey: conn.auth_config.api_key === "***",
    cfSecret: conn.auth_config.cf_access_secret === "***",
  };

  const initialForm = {
    name: conn.name,
    display_name: conn.display_name,
    type: conn.type as ConnectionType,
    url: conn.url,
    internal: conn.internal,
    bearer_token: conn.auth_config.bearer_token === "***" ? "" : (conn.auth_config.bearer_token ?? ""),
    api_key: conn.auth_config.api_key === "***" ? "" : (conn.auth_config.api_key ?? ""),
    api_key_header: conn.auth_config.api_key_header ?? "",
    cf_access_id: conn.auth_config.cf_access_id ?? "",
    cf_access_secret: conn.auth_config.cf_access_secret === "***" ? "" : (conn.auth_config.cf_access_secret ?? ""),
    enabled: conn.enabled,
  };

  async function handleSave(form: typeof EMPTY_FORM, authType: AuthType, endpoints: EndpointPermission[]) {
    const input: UpdateConnectionInput = {
      display_name: form.display_name,
      url: form.url,
      internal: form.internal,
      auth_config: buildAuthConfig(form, authType),
      endpoint_permissions: endpoints,
      enabled: form.enabled,
    };
    await update.mutateAsync(input);
    toast.success("Connection updated.");
    router.push(wsPaths.settings());
  }

  return (
    <ConnectionFormBody
      mode="edit"
      initial={initialForm}
      initialAuthType={detectAuthType(conn.auth_config)}
      initialEndpoints={conn.endpoint_permissions ?? []}
      existingConn={conn}
      secretsSet={secretsSet}
      onSave={(form, authType, endpoints) => {
        return handleSave(form, authType, endpoints).catch(() => {
          toast.error("Something went wrong. Please try again.");
        });
      }}
      isSaving={update.isPending}
    />
  );
}
