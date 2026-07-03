"use client";

import { useMemo, useState } from "react";
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

import type { Connection, ConnectionType, CreateConnectionInput, DefaultAccess, EndpointPermission, ScopableArg, UpdateConnectionInput } from "../types";
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
  session_exchange_enabled: false,
  session_exchange_path: "/sessions/exchange",
  session_exchange_ttl_seconds: "3600",
  enabled: true,
  default_access: "deny" as DefaultAccess,
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
  initialScopableArgs?: ScopableArg[];
  existingConn?: Connection;
  onSave: (
    form: typeof EMPTY_FORM,
    authType: AuthType,
    endpoints: EndpointPermission[],
    scopableArgs: ScopableArg[],
  ) => Promise<void>;
  isSaving: boolean;
  // Which secret fields had a value on load (server masks them as "***")
  secretsSet?: { bearerToken: boolean; apiKey: boolean; cfSecret: boolean };
}

function ConnectionFormBody({
  mode,
  initial,
  initialAuthType,
  initialEndpoints,
  initialScopableArgs,
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
  const [scopableArgs, setScopableArgs] = useState<ScopableArg[]>(initialScopableArgs ?? []);
  const [testResult, setTestResult] = useState<TestResult | null>(null);

  // Tool names known for this connection — the discovered list (persisted or from
  // a fresh test) — used to populate the tool / options-source dropdowns so an
  // admin picks from real tools instead of typing names by hand.
  const knownToolNames = useMemo(() => {
    const names = new Set<string>();
    for (const t of existingConn?.tools ?? []) if (t.name) names.add(t.name);
    for (const t of testResult?.tools ?? []) if (t.name) names.add(t.name);
    return [...names].sort();
  }, [existingConn?.tools, testResult?.tools]);

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

  function addScopableArg() {
    setScopableArgs((prev) => [
      ...prev,
      { tool: "", arg: "", options_source_tool: "", group_by: "folder", tag_field: "tags" },
    ]);
  }

  function removeScopableArg(index: number) {
    setScopableArgs((prev) => prev.filter((_, i) => i !== index));
  }

  function setScopableArgField(index: number, field: keyof ScopableArg, value: string) {
    setScopableArgs((prev) =>
      prev.map((sa, i) => (i === index ? { ...sa, [field]: value } : sa)),
    );
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    // Drop blank-path rows so an empty editor row doesn't persist as a junk endpoint.
    const cleaned = endpoints
      .map((ep) => ({ path: ep.path.trim(), methods: ep.methods }))
      .filter((ep) => ep.path !== "" && ep.methods.length > 0);
    // Drop inert scopable-arg rows (the server also normalizes, but keep the
    // payload clean): an entry needs a tool, an arg, and an options source.
    const cleanedArgs = scopableArgs
      .map((sa) => ({
        ...sa,
        tool: sa.tool.trim(),
        arg: sa.arg.trim(),
        options_source_tool: sa.options_source_tool.trim(),
      }))
      .filter((sa) => sa.tool !== "" && sa.arg !== "" && sa.options_source_tool !== "");
    await onSave(form, authType, cleaned, cleanedArgs);
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

          {/* Default access — baseline verdict for an actor with no explicit
              per-actor rule (FIR-2166 "C" v2). */}
          <div className="space-y-1.5">
            <Label htmlFor="conn-default-access">Default access</Label>
            <Select
              value={form.default_access}
              onValueChange={(v) => setForm((f) => ({ ...f, default_access: v as DefaultAccess }))}
            >
              <SelectTrigger id="conn-default-access">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="deny">Deny — closed; only explicitly allowed actors</SelectItem>
                <SelectItem value="ask">Ask — approval required unless explicitly allowed</SelectItem>
                <SelectItem value="allow">Allow — open unless an actor is explicitly denied</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              Baseline for actors with no explicit rule. Per-agent overrides still apply (Deny wins, then Allow, then Ask).
            </p>
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

          {form.type === "api" && (
            <div className="space-y-3 rounded-md border p-4">
              <div className="flex items-start gap-3">
                <Switch
                  id="conn-session-exchange"
                  checked={form.session_exchange_enabled}
                  onCheckedChange={(v) =>
                    setForm((f) => ({ ...f, session_exchange_enabled: v === true }))
                  }
                />
                <div>
                  <Label htmlFor="conn-session-exchange">Use personal session keys</Label>
                  <p className="text-xs text-muted-foreground">
                    Exchange the shared connection key for the triggering person's own short-lived key before each API call. If exchange fails, the call stops instead of falling back to the shared key.
                  </p>
                </div>
              </div>
              {form.session_exchange_enabled && (
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1.5">
                    <Label htmlFor="conn-session-exchange-path">Exchange path</Label>
                    <Input
                      id="conn-session-exchange-path"
                      value={form.session_exchange_path}
                      onChange={field("session_exchange_path")}
                      placeholder="/sessions/exchange"
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="conn-session-exchange-ttl">Key lifetime in seconds</Label>
                    <Input
                      id="conn-session-exchange-ttl"
                      type="number"
                      min={60}
                      value={form.session_exchange_ttl_seconds}
                      onChange={field("session_exchange_ttl_seconds")}
                      placeholder="3600"
                    />
                  </div>
                </div>
              )}
            </div>
          )}

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

          {/* Scopable arguments (MCP only) — declare which tool argument is the
              scoping axis for the tool-policy "When" picker, and which tool fills
              its choices. For the registry: query_run · data_source_id, options
              from data_sources_list. Empty = no scoping (unchanged behavior). */}
          {form.type === "mcp_http" && (
            <div className="space-y-3 rounded-md border p-4">
              <div className="flex items-start justify-between gap-2">
                <div>
                  <p className="text-sm font-medium">Scopable arguments</p>
                  <p className="text-xs text-muted-foreground">
                    Declare which tool argument can be scoped from a permission rule’s
                    “When”. The picker then offers a search + multi-select of allowed
                    values, grouped by folder. Leave empty for no scoping.
                    {knownToolNames.length > 0 ? (
                      <> Discovered tools: <span className="font-mono">{knownToolNames.join(", ")}</span>.</>
                    ) : (
                      <> Use “Test &amp; discover” to load this connection’s tools.</>
                    )}
                  </p>
                </div>
                <Button type="button" variant="outline" size="sm" onClick={addScopableArg} className="shrink-0">
                  <Plus className="size-3.5 mr-1" />
                  Add
                </Button>
              </div>

              {scopableArgs.length === 0 ? (
                <p className="text-xs text-muted-foreground italic">
                  No scopable arguments. Click “Add” to declare one (e.g. query_run ·
                  data_source_id from data_sources_list).
                </p>
              ) : (
                <div className="space-y-3">
                  {scopableArgs.map((sa, i) => (
                    <div key={i} className="space-y-2 rounded-md border p-3">
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-xs font-medium text-muted-foreground">Scope #{i + 1}</span>
                        <Button
                          type="button"
                          size="sm"
                          variant="ghost"
                          onClick={() => removeScopableArg(i)}
                          className="h-6 shrink-0 text-muted-foreground hover:text-destructive"
                          aria-label="Remove scopable argument"
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      </div>
                      <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
                        <div className="flex flex-col gap-1">
                          <Label className="text-[11px]">Tool</Label>
                          <Input
                            list="conn-tool-names"
                            value={sa.tool}
                            onChange={(e) => setScopableArgField(i, "tool", e.target.value)}
                            placeholder="query_run"
                            className="h-8 font-mono text-xs"
                          />
                        </div>
                        <div className="flex flex-col gap-1">
                          <Label className="text-[11px]">Argument</Label>
                          <Input
                            value={sa.arg}
                            onChange={(e) => setScopableArgField(i, "arg", e.target.value)}
                            placeholder="data_source_id"
                            className="h-8 font-mono text-xs"
                          />
                        </div>
                        <div className="flex flex-col gap-1">
                          <Label className="text-[11px]">Options from tool</Label>
                          <Input
                            list="conn-tool-names"
                            value={sa.options_source_tool}
                            onChange={(e) => setScopableArgField(i, "options_source_tool", e.target.value)}
                            placeholder="data_sources_list"
                            className="h-8 font-mono text-xs"
                          />
                        </div>
                      </div>
                    </div>
                  ))}
                  <datalist id="conn-tool-names">
                    {knownToolNames.map((n) => (
                      <option key={n} value={n} />
                    ))}
                  </datalist>
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

function buildAuthConfig(
  form: typeof EMPTY_FORM,
  authType: AuthType,
  existingAuth?: Connection["auth_config"],
): Connection["auth_config"] {
  const auth: Connection["auth_config"] = {
    ...(authType === "bearer" && form.bearer_token ? { bearer_token: form.bearer_token } : {}),
    ...(authType === "apikey" && form.api_key
      ? { api_key: form.api_key, ...(form.api_key_header ? { api_key_header: form.api_key_header } : {}) }
      : {}),
    ...(form.cf_access_id ? { cf_access_id: form.cf_access_id } : {}),
    ...(form.cf_access_secret ? { cf_access_secret: form.cf_access_secret } : {}),
  };
  if (authType === "bearer" && !auth.bearer_token && existingAuth?.bearer_token === "***") {
    auth.bearer_token = "***";
  }
  if (authType === "apikey" && !auth.api_key && existingAuth?.api_key === "***") {
    auth.api_key = "***";
    if (form.api_key_header) auth.api_key_header = form.api_key_header;
  }
  if (!auth.cf_access_secret && existingAuth?.cf_access_secret === "***") {
    auth.cf_access_secret = "***";
  }
  if (form.session_exchange_enabled) {
    const ttl = Number.parseInt(form.session_exchange_ttl_seconds, 10);
    auth.session_exchange = {
      enabled: true,
      ...(form.session_exchange_path.trim()
        ? { path: form.session_exchange_path.trim() }
        : {}),
      ...(Number.isFinite(ttl) && ttl > 0 ? { ttl_seconds: ttl } : {}),
    };
  }
  return auth;
}

// ---------------------------------------------------------------------------
// Public exports
// ---------------------------------------------------------------------------

export function ConnectionCreatePage() {
  const wsId = useWorkspaceId();
  const router = useNavigation();
  const wsPaths = useWorkspacePaths();
  const create = useCreateConnection(wsId ?? "");

  async function handleSave(
    form: typeof EMPTY_FORM,
    authType: AuthType,
    endpoints: EndpointPermission[],
    scopableArgs: ScopableArg[],
  ) {
    const input: CreateConnectionInput = {
      name: form.name,
      display_name: form.display_name,
      type: form.type,
      url: form.url,
      internal: form.internal,
      auth_config: buildAuthConfig(form, authType),
      endpoint_permissions: endpoints,
      scopable_args: scopableArgs,
      default_access: form.default_access,
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
      initialScopableArgs={[]}
      onSave={(form, authType, endpoints, scopableArgs) => {
        return handleSave(form, authType, endpoints, scopableArgs).catch(() => {
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
    session_exchange_enabled: conn.auth_config.session_exchange?.enabled === true,
    session_exchange_path: conn.auth_config.session_exchange?.path ?? "/sessions/exchange",
    session_exchange_ttl_seconds: String(conn.auth_config.session_exchange?.ttl_seconds ?? 3600),
    enabled: conn.enabled,
    default_access: conn.default_access,
  };
  const existingAuth = conn.auth_config;

  async function handleSave(
    form: typeof EMPTY_FORM,
    authType: AuthType,
    endpoints: EndpointPermission[],
    scopableArgs: ScopableArg[],
  ) {
    const input: UpdateConnectionInput = {
      display_name: form.display_name,
      url: form.url,
      internal: form.internal,
      auth_config: buildAuthConfig(form, authType, existingAuth),
      endpoint_permissions: endpoints,
      scopable_args: scopableArgs,
      enabled: form.enabled,
      default_access: form.default_access,
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
      initialScopableArgs={conn.scopable_args ?? []}
      existingConn={conn}
      secretsSet={secretsSet}
      onSave={(form, authType, endpoints, scopableArgs) => {
        return handleSave(form, authType, endpoints, scopableArgs).catch(() => {
          toast.error("Something went wrong. Please try again.");
        });
      }}
      isSaving={update.isPending}
    />
  );
}
