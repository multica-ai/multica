"use client";

import { useEffect, useState } from "react";
import { Globe, Plus, Trash2, ShieldOff } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Badge } from "@multica/ui/components/ui/badge";
import { RadioGroup, RadioGroupItem } from "@multica/ui/components/ui/radio-group";
import { useCurrentMember } from "@multica/core/permissions";
import { useWorkspaceId } from "@multica/core/hooks";

import { useWebFetchPolicy, useUpdateWebFetchPolicy } from "../queries";

type Mode = "allow" | "disallow";

// Bare host or subdomain wildcard, e.g. github.com or *.github.com. Mirrors the
// server-side rule grammar (webfetchpolicy.HostMatchesRule).
const RULE_RE = /^(\*\.)?[a-z0-9-]+(\.[a-z0-9-]+)+$/;

function normalizeRule(raw: string): string {
  return raw.trim().toLowerCase().replace(/^https?:\/\//, "").replace(/\/.*$/, "");
}

export function WebFetchPolicySettingsTab() {
  const wsId = useWorkspaceId();
  const { role, isLoading: isMemberLoading } = useCurrentMember(wsId);
  const { data: policy, isLoading } = useWebFetchPolicy(wsId);
  const update = useUpdateWebFetchPolicy(wsId);

  const [mode, setMode] = useState<Mode>("allow");
  const [rules, setRules] = useState<string[]>([]);
  const [draft, setDraft] = useState("");
  const [hydrated, setHydrated] = useState(false);

  // Seed local form state from the server once the policy loads.
  useEffect(() => {
    if (policy && !hydrated) {
      setMode(policy.mode);
      setRules(policy.rules);
      setHydrated(true);
    }
  }, [policy, hydrated]);

  if (!wsId || isMemberLoading || isLoading) {
    return (
      <div className="flex h-32 items-center justify-center text-sm text-muted-foreground">
        Loading…
      </div>
    );
  }

  const isAdmin = role === "owner" || role === "admin";
  if (!isAdmin) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 p-6 text-center">
        <ShieldOff className="size-8 text-muted-foreground" />
        <h2 className="text-base font-medium">Access denied</h2>
        <p className="max-w-sm text-sm text-muted-foreground">
          Only workspace owners and admins can manage the web fetch policy.
        </p>
      </div>
    );
  }

  function addRule() {
    const r = normalizeRule(draft);
    if (!r) return;
    if (!RULE_RE.test(r)) {
      toast.error("Enter a host like github.com or *.github.com.");
      return;
    }
    if (rules.includes(r)) {
      toast.error(`"${r}" is already in the list.`);
      setDraft("");
      return;
    }
    setRules([...rules, r]);
    setDraft("");
  }

  function removeRule(r: string) {
    setRules(rules.filter((x) => x !== r));
  }

  async function save() {
    try {
      await update.mutateAsync({ mode, rules });
      toast.success("Web fetch policy saved.");
    } catch {
      toast.error("Save failed. Please try again.");
    }
  }

  const dirty =
    !policy ||
    mode !== policy.mode ||
    rules.length !== policy.rules.length ||
    rules.some((r, i) => r !== policy.rules[i]);

  return (
    <div className="max-w-2xl space-y-6">
      <div className="flex items-center gap-2">
        <Globe className="size-5 text-muted-foreground" />
        <div>
          <h2 className="text-base font-semibold">Web fetch</h2>
          <p className="text-sm text-muted-foreground">
            Control which URLs agents may fetch with the <code>web_fetch</code> tool.
            {policy && !policy.configured ? " This workspace is using the default list." : null}
          </p>
        </div>
      </div>

      <div className="space-y-3">
        <Label>Mode</Label>
        <RadioGroup value={mode} onValueChange={(v) => setMode(v as Mode)} className="gap-3">
          <label className="flex cursor-pointer items-start gap-3">
            <RadioGroupItem value="allow" className="mt-0.5" />
            <span>
              <span className="text-sm font-medium">Allow-list</span>
              <span className="block text-sm text-muted-foreground">
                Agents may fetch <strong>only</strong> the hosts listed below.
              </span>
            </span>
          </label>
          <label className="flex cursor-pointer items-start gap-3">
            <RadioGroupItem value="disallow" className="mt-0.5" />
            <span>
              <span className="text-sm font-medium">Disallow-list</span>
              <span className="block text-sm text-muted-foreground">
                Agents may fetch <strong>any</strong> host except the ones listed below.
              </span>
            </span>
          </label>
        </RadioGroup>
      </div>

      <div className="space-y-3">
        <Label htmlFor="web-fetch-rule">
          {mode === "allow" ? "Allowed hosts" : "Blocked hosts"}
        </Label>
        <div className="flex gap-2">
          <Input
            id="web-fetch-rule"
            placeholder="github.com or *.github.com"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addRule();
              }
            }}
          />
          <Button type="button" variant="outline" onClick={addRule}>
            <Plus className="size-4" /> Add
          </Button>
        </div>

        {rules.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No hosts yet.{" "}
            {mode === "allow"
              ? "With an empty allow-list, agents cannot fetch any URL."
              : "With an empty disallow-list, agents can fetch any URL."}
          </p>
        ) : (
          <ul className="flex flex-wrap gap-2">
            {rules.map((r) => (
              <li key={r}>
                <Badge variant="secondary" className="gap-1.5 py-1 pr-1 pl-2">
                  <span className="font-mono text-xs">{r}</span>
                  <button
                    type="button"
                    aria-label={`Remove ${r}`}
                    onClick={() => removeRule(r)}
                    className="rounded-sm p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
                  >
                    <Trash2 className="size-3.5" />
                  </button>
                </Badge>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="flex justify-end">
        <Button onClick={save} disabled={!dirty || update.isPending}>
          {update.isPending ? "Saving…" : "Save changes"}
        </Button>
      </div>
    </div>
  );
}
