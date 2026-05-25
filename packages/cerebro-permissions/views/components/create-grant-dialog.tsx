"use client";

import { useState } from "react";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  capabilityDescription,
  capabilityLabel,
  DANGEROUS_CAPABILITIES,
  DATA_CAPABILITIES,
  DEFAULT_CAPABILITY_KEY,
  GRANT_CLASSIFICATIONS,
  useCreatePersonaGrant,
  type CreatePersonaGrantRequest,
  type GrantSubjectType,
} from "../../core";

interface CreateGrantDialogProps {
  wsId: string;
  onClose: () => void;
}

const SUBJECT_TYPES: GrantSubjectType[] = [
  "actor",
  "agent",
  "member",
  "group",
  "role",
  "workspace_default",
  "anonymous",
];

const RESOURCE_TYPES = [
  "issue",
  "project",
  "agent",
  "runtime",
  "repo",
  "mcp_server",
  "api_token",
  "secret",
  "webhook",
  "skill",
];

export function CreateGrantDialog({ wsId: _wsId, onClose }: CreateGrantDialogProps) {
  const [subjectType, setSubjectType] = useState<GrantSubjectType>("group");
  const [subjectId, setSubjectId] = useState("");
  const [subjectName, setSubjectName] = useState("");
  const [resourceType, setResourceType] = useState("issue");
  const [resourcePattern, setResourcePattern] = useState("*");
  const [capability, setCapability] = useState(DEFAULT_CAPABILITY_KEY);
  const [classification, setClassification] = useState<string>("unclassified");
  const [approvalRequired, setApprovalRequired] = useState(false);
  const [endsAt, setEndsAt] = useState("");
  const [description, setDescription] = useState("");

  const create = useCreatePersonaGrant();

  // workspace_default + anonymous have no subject id — make those forms
  // valid even when subjectId is empty.
  const subjectFreeOfId =
    subjectType === "workspace_default" || subjectType === "anonymous";

  const canSubmit =
    capability.trim().length > 0 &&
    resourcePattern.trim().length > 0 &&
    (subjectFreeOfId || subjectId.trim().length > 0) &&
    !create.isPending;

  const handleSubmit = async () => {
    if (!canSubmit) return;
    const body: CreatePersonaGrantRequest = {
      subject: {
        type: subjectType,
        id: subjectFreeOfId ? null : subjectId.trim(),
        display_name: subjectName.trim() || null,
      },
      resource: {
        type: resourceType,
        pattern: resourcePattern.trim(),
      },
      capability: capability.trim(),
      classification_ceiling: classification,
      approval_required: approvalRequired,
      time_window: endsAt
        ? { starts_at: null, ends_at: new Date(endsAt).toISOString() }
        : null,
      description: description.trim() || null,
    };
    try {
      await create.mutateAsync(body);
      toast.success("Grant created");
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Couldn't create grant",
      );
    }
  };

  return (
    <Dialog open={true} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>New grant</DialogTitle>
          <DialogDescription>
            Give a subject a capability on a resource. The audit log records the action with your user as the actor.
          </DialogDescription>
        </DialogHeader>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            void handleSubmit();
          }}
          noValidate
        >
        <div className="grid gap-4 py-2 text-sm">
          <div className="grid grid-cols-3 gap-2">
            <Field label="Subject type">
              <Select
                value={subjectType}
                onValueChange={(v) => {
                  if (v) setSubjectType(v as GrantSubjectType);
                }}
              >
                <SelectTrigger size="sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {SUBJECT_TYPES.map((t) => (
                    <SelectItem key={t} value={t}>
                      {t}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="Subject ID">
              <Input
                value={subjectId}
                onChange={(e) => setSubjectId(e.target.value)}
                placeholder={subjectFreeOfId ? "n/a" : "UUID or slug"}
                disabled={subjectFreeOfId}
                aria-label="Subject ID"
              />
            </Field>
            <Field label="Display name (optional)">
              <Input
                value={subjectName}
                onChange={(e) => setSubjectName(e.target.value)}
                placeholder="e.g. Marketing"
                aria-label="Subject display name"
              />
            </Field>
          </div>

          <div className="grid grid-cols-3 gap-2">
            <Field label="Resource type">
              <Select
                value={resourceType}
                onValueChange={(v) => {
                  if (v) setResourceType(v);
                }}
              >
                <SelectTrigger size="sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {RESOURCE_TYPES.map((t) => (
                    <SelectItem key={t} value={t}>
                      {t}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="Pattern">
              <Input
                value={resourcePattern}
                onChange={(e) => setResourcePattern(e.target.value)}
                placeholder="* or UUID/glob"
                aria-label="Resource pattern"
              />
            </Field>
            <Field label="Action">
              <Select
                value={capability}
                onValueChange={(v) => {
                  if (v) setCapability(v);
                }}
              >
                <SelectTrigger size="sm" aria-label="Capability">
                  <SelectValue>{capabilityLabel(capability)}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectLabel>Sensitive actions</SelectLabel>
                    {DANGEROUS_CAPABILITIES.map((c) => (
                      <SelectItem key={c.key} value={c.key}>
                        {c.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                  <SelectGroup>
                    <SelectLabel>Multica data</SelectLabel>
                    {DATA_CAPABILITIES.map((c) => (
                      <SelectItem key={c.key} value={c.key}>
                        {c.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
          </div>

          {capabilityDescription(capability) && (
            <p className="-mt-2 text-xs text-muted-foreground" role="note">
              {capabilityDescription(capability)}
            </p>
          )}

          <div className="grid grid-cols-2 gap-2">
            <Field label="Classification ceiling">
              <Select
                value={classification}
                onValueChange={(v) => {
                  if (v) setClassification(v);
                }}
              >
                <SelectTrigger size="sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {GRANT_CLASSIFICATIONS.map((c) => (
                    <SelectItem key={c} value={c}>
                      {c}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="Expires">
              <Input
                type="datetime-local"
                value={endsAt}
                onChange={(e) => setEndsAt(e.target.value)}
                aria-label="Expires"
              />
            </Field>
          </div>

          <Field label="Requires approval">
            <div className="flex items-center gap-2">
              <Switch
                checked={approvalRequired}
                onCheckedChange={setApprovalRequired}
                aria-label="Requires approval"
              />
              <span className="text-xs text-muted-foreground">
                Workspace admin must approve every use of this capability before it runs.
              </span>
            </div>
          </Field>

          <Field label="Description (optional)">
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What is this grant for?"
              rows={2}
            />
          </Field>
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={onClose}
            disabled={create.isPending}
          >
            Cancel
          </Button>
          <Button type="submit" disabled={!canSubmit}>
            {create.isPending ? "Creating…" : "Create grant"}
          </Button>
        </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs font-medium text-muted-foreground">
        {label}
      </span>
      {children}
    </label>
  );
}
