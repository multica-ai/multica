"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Loader2, Trash2 } from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
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
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  GRANT_CLASSIFICATIONS,
  grantDetailOptions,
  useUpdatePersonaGrant,
  type PersonaGrant,
  type UpdatePersonaGrantRequest,
} from "../../core";

interface GrantDrawerProps {
  wsId: string;
  grantId: string;
  onClose: () => void;
}

export function GrantDrawer({ wsId, grantId, onClose }: GrantDrawerProps) {
  const { data: grant, isPending, error } = useQuery(
    grantDetailOptions(wsId, grantId),
  );

  return (
    <Sheet open={true} onOpenChange={(o) => !o && onClose()}>
      <SheetContent
        side="right"
        className="w-full max-w-[520px] overflow-y-auto p-0"
      >
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle className="text-sm font-semibold">Grant detaljer</SheetTitle>
          <SheetDescription className="text-xs">
            Justér klassifikation, tidsvindue, approval-krav eller status.
          </SheetDescription>
        </SheetHeader>

        <div className="p-5">
          {isPending && (
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" />
              Indlæser…
            </div>
          )}

          {error && (
            <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
              Kunne ikke hente grant:{" "}
              {error instanceof Error ? error.message : "ukendt fejl"}
            </div>
          )}

          {grant && (
            <GrantEditor wsId={wsId} grantId={grantId} initial={grant} onDone={onClose} />
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

interface GrantEditorProps {
  wsId: string;
  grantId: string;
  initial: PersonaGrant;
  onDone: () => void;
}

// Inline editor for an existing grant. Only the writable subset (status,
// classification, approval, time window, description) is exposed; the
// subject + resource + capability are immutable here — delete and recreate
// if the admin needs a different shape.
function GrantEditor({
  wsId: _wsId,
  grantId,
  initial,
  onDone,
}: GrantEditorProps) {
  const g = initial;

  const [classification, setClassification] = useState<string>(
    g.classification_ceiling,
  );
  const [status, setStatus] = useState<string>(g.status);
  const [approval, setApproval] = useState<boolean>(g.approval_required);
  const [startsAt, setStartsAt] = useState<string>(
    g.time_window?.starts_at ?? "",
  );
  const [endsAt, setEndsAt] = useState<string>(g.time_window?.ends_at ?? "");
  const [description, setDescription] = useState<string>(g.description ?? "");
  const [confirmDelete, setConfirmDelete] = useState(false);

  // Refresh local state if the query refetches with newer data.
  useEffect(() => {
    setClassification(g.classification_ceiling);
    setStatus(g.status);
    setApproval(g.approval_required);
    setStartsAt(g.time_window?.starts_at ?? "");
    setEndsAt(g.time_window?.ends_at ?? "");
    setDescription(g.description ?? "");
  }, [g]);

  const update = useUpdatePersonaGrant();

  const handleSave = async () => {
    const body: UpdatePersonaGrantRequest = {
      classification_ceiling: classification,
      status,
      approval_required: approval,
      time_window:
        startsAt || endsAt
          ? {
              starts_at: startsAt || null,
              ends_at: endsAt || null,
            }
          : null,
      description: description.trim() || null,
    };
    try {
      await update.mutateAsync({ id: grantId, body });
      toast.success("Grant opdateret");
      onDone();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Kunne ikke opdatere grant");
    }
  };

  const handleDelete = async () => {
    try {
      await update.mutateAsync({ id: grantId, body: { status: "revoked" } });
      toast.success("Grant tilbagekaldt");
      setConfirmDelete(false);
      onDone();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Kunne ikke tilbagekalde grant");
    }
  };

  return (
    <div className="space-y-5 text-sm">
      <ReadOnlySection
        rows={[
          {
            label: "Subjekt",
            value: `${g.subject.type} · ${
              g.subject.display_name ?? g.subject.id ?? "—"
            }`,
          },
          {
            label: "Ressource",
            value: `${g.resource.type} : ${g.resource.pattern}`,
          },
          { label: "Kapabilitet", value: g.capability },
          { label: "Oprettet", value: formatDateTime(g.created_at) },
          { label: "Opdateret", value: formatDateTime(g.updated_at) },
        ]}
      />

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

      <Field label="Status">
        <Select
          value={status}
          onValueChange={(v) => {
            if (v) setStatus(v);
          }}
        >
          <SelectTrigger size="sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="active">Aktiv</SelectItem>
            <SelectItem value="pending_approval">Afventer approval</SelectItem>
            <SelectItem value="expired">Udløbet</SelectItem>
            <SelectItem value="revoked">Tilbagekaldt</SelectItem>
          </SelectContent>
        </Select>
      </Field>

      <Field label="Approval påkrævet">
        <div className="flex items-center gap-2">
          <Switch
            checked={approval}
            onCheckedChange={setApproval}
            aria-label="Approval påkrævet"
          />
          <span className="text-xs text-muted-foreground">
            Subjektet skal ind via approval-flow før kapabiliteten bruges.
          </span>
        </div>
      </Field>

      <Field label="Tidsvindue">
        <div className="grid grid-cols-2 gap-2">
          <Input
            type="datetime-local"
            value={toLocalInput(startsAt)}
            onChange={(e) => setStartsAt(fromLocalInput(e.target.value))}
            aria-label="Starter"
          />
          <Input
            type="datetime-local"
            value={toLocalInput(endsAt)}
            onChange={(e) => setEndsAt(fromLocalInput(e.target.value))}
            aria-label="Slutter"
          />
        </div>
        <p className="mt-1 text-[11px] text-muted-foreground">
          Tomme felter = grant gælder uden tidsbegrænsning.
        </p>
      </Field>

      <Field label="Beskrivelse">
        <Textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Hvorfor er denne adgang nødvendig?"
          rows={3}
        />
      </Field>

      <div className="flex items-center justify-between gap-2 pt-2">
        <Button
          variant="ghost"
          size="sm"
          className="text-destructive hover:bg-destructive/10 hover:text-destructive"
          onClick={() => setConfirmDelete(true)}
          aria-label="Tilbagekald grant"
        >
          <Trash2 className="size-4" />
          Tilbagekald grant
        </Button>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={onDone}>
            Annullér
          </Button>
          <Button
            size="sm"
            onClick={handleSave}
            disabled={update.isPending}
          >
            {update.isPending ? "Gemmer…" : "Gem ændringer"}
          </Button>
        </div>
      </div>

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Tilbagekald grant?</AlertDialogTitle>
            <AlertDialogDescription>
              {g.subject.display_name ?? g.subject.id ?? "Subjektet"} mister
              kapabiliteten "{g.capability}" på {g.resource.type}:
              {g.resource.pattern}. Dette kan ikke fortrydes.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={update.isPending}>
              Annullér
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                void handleDelete();
              }}
              disabled={update.isPending}
            >
              {update.isPending ? "Tilbagekalder…" : "Tilbagekald"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
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

function ReadOnlySection({
  rows,
}: {
  rows: { label: string; value: string }[];
}) {
  return (
    <dl className="grid grid-cols-[7rem_1fr] gap-x-3 gap-y-1.5 rounded-md border bg-muted/30 p-3 text-xs">
      {rows.map((r) => (
        <div key={r.label} className="contents">
          <dt className="text-muted-foreground">{r.label}</dt>
          <dd className="truncate font-mono">{r.value}</dd>
        </div>
      ))}
    </dl>
  );
}

function formatDateTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return iso;
  }
}

// Convert ISO → local datetime-local input value (yyyy-MM-ddTHH:mm).
function toLocalInput(iso: string): string {
  if (!iso) return "";
  try {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "";
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  } catch {
    return "";
  }
}

function fromLocalInput(v: string): string {
  if (!v) return "";
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? "" : d.toISOString();
}
