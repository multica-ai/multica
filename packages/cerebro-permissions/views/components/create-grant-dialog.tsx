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
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
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

const CAPABILITIES = [
  "issue.read",
  "issue.write",
  "project.read",
  "project.write",
  "agent.run",
  "repo.push",
  "secret.use",
];

export function CreateGrantDialog({ wsId: _wsId, onClose }: CreateGrantDialogProps) {
  const [subjectType, setSubjectType] = useState<GrantSubjectType>("group");
  const [subjectId, setSubjectId] = useState("");
  const [subjectName, setSubjectName] = useState("");
  const [resourceType, setResourceType] = useState("issue");
  const [resourcePattern, setResourcePattern] = useState("*");
  const [capability, setCapability] = useState("issue.read");
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
      toast.success("Grant oprettet");
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Kunne ikke oprette grant",
      );
    }
  };

  return (
    <Dialog open={true} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Nyt grant</DialogTitle>
          <DialogDescription>
            Giv et subjekt en kapabilitet på en ressource. Audit logger
            handlingen som "{wsActorPlaceholder()} via UI oprettede grant".
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
            <Field label="Subjekt-type">
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
            <Field label="Subjekt-ID">
              <Input
                value={subjectId}
                onChange={(e) => setSubjectId(e.target.value)}
                placeholder={subjectFreeOfId ? "n/a" : "UUID eller slug"}
                disabled={subjectFreeOfId}
                aria-label="Subjekt-ID"
              />
            </Field>
            <Field label="Vis-navn (valgfri)">
              <Input
                value={subjectName}
                onChange={(e) => setSubjectName(e.target.value)}
                placeholder="fx Marketing"
                aria-label="Subjekt vis-navn"
              />
            </Field>
          </div>

          <div className="grid grid-cols-3 gap-2">
            <Field label="Ressource-type">
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
                placeholder="* eller UUID/glob"
                aria-label="Ressource-pattern"
              />
            </Field>
            <Field label="Kapabilitet">
              <Select
                value={capability}
                onValueChange={(v) => {
                  if (v) setCapability(v);
                }}
              >
                <SelectTrigger size="sm" aria-label="Kapabilitet">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {CAPABILITIES.map((c) => (
                    <SelectItem key={c} value={c}>
                      {c}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          </div>

          <div className="grid grid-cols-2 gap-2">
            <Field label="Klassifikations-loft">
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
            <Field label="Udløber">
              <Input
                type="datetime-local"
                value={endsAt}
                onChange={(e) => setEndsAt(e.target.value)}
                aria-label="Udløber"
              />
            </Field>
          </div>

          <Field label="Approval påkrævet">
            <div className="flex items-center gap-2">
              <Switch
                checked={approvalRequired}
                onCheckedChange={setApprovalRequired}
                aria-label="Approval påkrævet"
              />
              <span className="text-xs text-muted-foreground">
                Persona spørger workspace-admin før kapabiliteten må bruges.
              </span>
            </div>
          </Field>

          <Field label="Beskrivelse (valgfri)">
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Hvad er grantet til?"
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
            Annullér
          </Button>
          <Button type="submit" disabled={!canSubmit}>
            {create.isPending ? "Opretter…" : "Opret grant"}
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

function wsActorPlaceholder(): string {
  // Audit trail is "actor via flade gjorde X" — we don't have the current
  // user's name in this scope; the server fills in the real actor from the
  // auth context. Placeholder is only for the description copy.
  return "du";
}
