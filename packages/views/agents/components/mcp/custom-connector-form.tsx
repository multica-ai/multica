"use client";

import { useMemo, useState } from "react";
import { Loader2, Plus } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import { mcpConnectorKeys } from "@multica/core/agents";
import { api } from "@multica/core/api";
import type { CreateMcpConnectorRequest } from "@multica/core/types";
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
import { Textarea } from "@multica/ui/components/ui/textarea";
import { toast } from "sonner";

/**
 * Determines whether the current user may author workspace-custom connectors.
 * Mirrors the admin gate used across settings tabs (`workspace-tab`,
 * `lark-tab`): owner or admin role on the active workspace. Keyed on `wsId` so
 * switching workspace re-evaluates the role automatically.
 */
function useCanAuthorConnectors(wsId: string): boolean {
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  return useMemo(() => {
    const member = members.find((m) => m.user_id === user?.id) ?? null;
    return member?.role === "owner" || member?.role === "admin";
  }, [members, user?.id]);
}

/**
 * Admin-only "Add custom connector" entry rendered inside the connector
 * directory header (via its `customAction` slot). Non-admins render nothing —
 * the directory itself stays role-agnostic. Clicking opens the authoring form.
 */
export function CustomConnectorEntry({ wsId }: { wsId: string }) {
  const canAuthor = useCanAuthorConnectors(wsId);
  const [open, setOpen] = useState(false);

  if (!canAuthor) return null;

  return (
    <>
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() => setOpen(true)}
        className="shrink-0"
      >
        <Plus className="h-3.5 w-3.5" />
        Add custom connector
      </Button>
      <CustomConnectorForm wsId={wsId} open={open} onOpenChange={setOpen} />
    </>
  );
}

/**
 * Authoring form for a workspace-custom connector. Submits through
 * `api.createMcpConnector` and, on success, invalidates the connector list so
 * the new entry shows up in the directory immediately. `mcp_template` is taken
 * as raw JSON and validated before submit — an invalid template surfaces an
 * inline error rather than failing server-side.
 */
export function CustomConnectorForm({
  wsId,
  open,
  onOpenChange,
}: {
  wsId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [description, setDescription] = useState("");
  const [templateText, setTemplateText] = useState("");

  const trimmedTemplate = templateText.trim();
  const templateParse = useMemo<
    { ok: true; value: unknown } | { ok: false; error: string }
  >(() => {
    if (trimmedTemplate === "")
      return { ok: false, error: "Template is required." };
    try {
      const value = JSON.parse(trimmedTemplate);
      if (value === null || typeof value !== "object" || Array.isArray(value)) {
        return { ok: false, error: "Template must be a JSON object." };
      }
      return { ok: true, value };
    } catch (err) {
      return {
        ok: false,
        error: err instanceof Error ? err.message : "Invalid JSON.",
      };
    }
  }, [trimmedTemplate]);

  const mutation = useMutation({
    mutationFn: (data: CreateMcpConnectorRequest) =>
      api.createMcpConnector(wsId, data),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: mcpConnectorKeys.list(wsId) });
      toast.success("Custom connector added");
      reset();
      onOpenChange(false);
    },
    onError: (err) => {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : "Failed to add connector",
      );
    },
  });

  const reset = () => {
    setName("");
    setSlug("");
    setDescription("");
    setTemplateText("");
  };

  const canSubmit =
    name.trim() !== "" &&
    slug.trim() !== "" &&
    templateParse.ok &&
    !mutation.isPending;

  const handleSubmit = () => {
    if (!canSubmit || !templateParse.ok) return;
    mutation.mutate({
      slug: slug.trim(),
      name: name.trim(),
      description: description.trim() || null,
      mcp_template: templateParse.value,
    });
  };

  const showTemplateError = trimmedTemplate !== "" && !templateParse.ok;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) reset();
        onOpenChange(next);
      }}
    >
      <DialogContent className="w-full max-w-md">
        <DialogHeader>
          <DialogTitle>Add custom connector</DialogTitle>
          <DialogDescription>
            Author a Model Context Protocol connector for this workspace. It
            appears in the directory for everyone here.
          </DialogDescription>
        </DialogHeader>

        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            handleSubmit();
          }}
        >
          <div className="space-y-1.5">
            <Label htmlFor="custom-connector-name">
              Name<span className="ml-0.5 text-destructive">*</span>
            </Label>
            <Input
              id="custom-connector-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Internal Wiki"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="custom-connector-slug">
              Slug<span className="ml-0.5 text-destructive">*</span>
            </Label>
            <Input
              id="custom-connector-slug"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              placeholder="internal-wiki"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="custom-connector-description">Description</Label>
            <Input
              id="custom-connector-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What this connector does"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="custom-connector-template">
              MCP template (JSON)
              <span className="ml-0.5 text-destructive">*</span>
            </Label>
            <Textarea
              id="custom-connector-template"
              value={templateText}
              onChange={(e) => setTemplateText(e.target.value)}
              placeholder={
                '{\n  "mcpServers": {\n    "wiki": { "command": "npx", "args": ["wiki-mcp"] }\n  }\n}'
              }
              aria-invalid={showTemplateError || undefined}
              spellCheck={false}
              className="min-h-[140px] font-mono text-xs"
            />
            {showTemplateError && (
              <p className="text-xs text-destructive">{templateParse.error}</p>
            )}
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={!canSubmit}>
              {mutation.isPending ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : null}
              Add connector
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
