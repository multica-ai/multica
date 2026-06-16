"use client";

import { useId, useMemo, useState } from "react";
import type { FormEvent } from "react";
import {
  ChevronLeft,
  Loader2,
  Pencil,
  Plus,
  Server,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type {
  RuntimeProfile,
  RuntimeProfileVisibility,
  RuntimeProtocolFamily,
} from "@multica/core/types";
import {
  runtimeProfileListOptions,
  useCreateRuntimeProfile,
  useUpdateRuntimeProfile,
} from "@multica/core/runtimes";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { ProviderLogo } from "./provider-logo";
import { DeleteRuntimeProfileDialog } from "./delete-runtime-profile-dialog";
import {
  PROTOCOL_FAMILIES,
  buildRuntimeCatalog,
  validateProfileForm,
  type ProfileFormErrorField,
  type ProfileFormValues,
  type RuntimeCatalogEntry,
} from "./runtime-profile-catalog";
import { useT } from "../../i18n";

// The dialog runs in two surfaces that swap inside one Popup:
//   - "browse": master list (built-in + custom, badged) + adaptive detail
//   - "form":   create (2-step) or edit (single step, family locked)
type DialogState =
  | { surface: "browse" }
  | { surface: "form"; mode: "create"; step: "family" | "details" }
  | { surface: "form"; mode: "edit"; profile: RuntimeProfile };

export function RuntimeProfilesDialog({
  wsId,
  onClose,
}: {
  wsId: string;
  onClose: () => void;
}) {
  const { t } = useT("runtimes");
  const { data: profiles = [], isLoading } = useQuery(
    runtimeProfileListOptions(wsId),
  );

  const [state, setState] = useState<DialogState>({ surface: "browse" });
  const [selectedId, setSelectedId] = useState<string | null>(null);
  // Carries the chosen family from create-step-1 into the form.
  const [draftFamily, setDraftFamily] =
    useState<RuntimeProtocolFamily>(PROTOCOL_FAMILIES[0] ?? "claude");

  const catalog = useMemo(() => buildRuntimeCatalog(profiles), [profiles]);
  const selectedEntry =
    catalog.find((entry) => entry.id === selectedId) ?? null;

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[88vh] flex-col gap-0 p-0 sm:max-w-3xl">
        <DialogHeader className="border-b px-6 py-5">
          <DialogTitle className="flex items-center gap-2 text-base">
            <Server className="h-4 w-4 text-muted-foreground" />
            {t(($) => $.profiles.dialog_title)}
          </DialogTitle>
          <DialogDescription className="text-xs">
            {t(($) => $.profiles.dialog_description)}
          </DialogDescription>
        </DialogHeader>

        {state.surface === "form" ? (
          <ProfileFormView
            wsId={wsId}
            mode={state.mode}
            step={state.mode === "create" ? state.step : "details"}
            family={
              state.mode === "edit" ? state.profile.protocol_family : draftFamily
            }
            profile={state.mode === "edit" ? state.profile : null}
            onPickFamily={(family) => {
              setDraftFamily(family);
              setState({ surface: "form", mode: "create", step: "details" });
            }}
            onBack={() => {
              if (state.mode === "create" && state.step === "details") {
                setState({ surface: "form", mode: "create", step: "family" });
              } else {
                setState({ surface: "browse" });
              }
            }}
            onCancel={() => setState({ surface: "browse" })}
            onSaved={(profile) => {
              setSelectedId(profile.id);
              setState({ surface: "browse" });
            }}
          />
        ) : (
          <div className="grid min-h-0 flex-1 grid-cols-1 md:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
            <CatalogList
              entries={catalog}
              loading={isLoading}
              selectedId={selectedId}
              onSelect={setSelectedId}
              onAddNew={() =>
                setState({ surface: "form", mode: "create", step: "family" })
              }
            />
            <DetailPanel
              entry={selectedEntry}
              wsId={wsId}
              onEdit={(profile) =>
                setState({ surface: "form", mode: "edit", profile })
              }
              onDeleted={() => setSelectedId(null)}
            />
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Master list — built-in families + custom profiles, mixed, each badged.
// ---------------------------------------------------------------------------

function CatalogList({
  entries,
  loading,
  selectedId,
  onSelect,
  onAddNew,
}: {
  entries: RuntimeCatalogEntry[];
  loading: boolean;
  selectedId: string | null;
  onSelect: (id: string) => void;
  onAddNew: () => void;
}) {
  const { t } = useT("runtimes");
  const hasCustom = entries.some((entry) => entry.kind === "custom");

  return (
    <div className="flex min-h-0 flex-col border-b md:border-b-0 md:border-r">
      <div className="flex shrink-0 items-center justify-between border-b bg-background px-4 py-2.5">
        <h3 className="text-sm font-medium">
          {t(($) => $.profiles.list_title)}
        </h3>
        <Button type="button" size="sm" className="h-7 px-2" onClick={onAddNew}>
          <Plus className="h-3.5 w-3.5" />
          {t(($) => $.profiles.add_new)}
        </Button>
      </div>
      {loading ? (
        <div className="flex h-40 items-center justify-center">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      ) : (
        <ul className="min-h-0 flex-1 overflow-y-auto py-1" role="listbox" aria-label={t(($) => $.profiles.list_title)}>
          {entries.map((entry) => (
            <li key={entry.id}>
              <CatalogRow
                entry={entry}
                active={entry.id === selectedId}
                onClick={() => onSelect(entry.id)}
              />
            </li>
          ))}
          {!hasCustom && (
            <li className="px-4 py-3 text-xs text-muted-foreground">
              {t(($) => $.profiles.empty_custom)}
            </li>
          )}
        </ul>
      )}
    </div>
  );
}

function CatalogRow({
  entry,
  active,
  onClick,
}: {
  entry: RuntimeCatalogEntry;
  active: boolean;
  onClick: () => void;
}) {
  const { t } = useT("runtimes");
  const label =
    entry.kind === "custom" ? entry.profile.display_name : entry.protocolFamily;
  const disabled = entry.kind === "custom" && !entry.profile.enabled;
  return (
    <button
      type="button"
      role="option"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "flex w-full min-w-0 items-center gap-2.5 px-4 py-2 text-left transition-colors",
        active ? "bg-accent" : "hover:bg-accent/50",
      )}
    >
      <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md border bg-background">
        <ProviderLogo provider={entry.protocolFamily} className="h-4 w-4" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-1.5">
          <span
            className={cn(
              "truncate text-sm font-medium",
              entry.kind === "builtin" && "capitalize",
            )}
          >
            {label}
          </span>
          {disabled && (
            <span className="shrink-0 rounded bg-muted px-1 text-[10px] font-medium text-muted-foreground">
              {t(($) => $.profiles.badge_disabled)}
            </span>
          )}
        </span>
        {entry.kind === "custom" && (
          <span className="block truncate text-xs capitalize text-muted-foreground">
            {entry.protocolFamily}
          </span>
        )}
      </span>
      <KindBadge kind={entry.kind} />
    </button>
  );
}

function KindBadge({ kind }: { kind: "builtin" | "custom" }) {
  const { t } = useT("runtimes");
  return (
    <span
      className={cn(
        "shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium",
        kind === "custom"
          ? "bg-info/10 text-info"
          : "bg-muted text-muted-foreground",
      )}
    >
      {kind === "custom"
        ? t(($) => $.profiles.badge_custom)
        : t(($) => $.profiles.badge_builtin)}
    </span>
  );
}

// ---------------------------------------------------------------------------
// Detail panel — adaptive: built-in is read-only, custom shows fields +
// Edit / Delete.
// ---------------------------------------------------------------------------

function DetailPanel({
  entry,
  wsId,
  onEdit,
  onDeleted,
}: {
  entry: RuntimeCatalogEntry | null;
  wsId: string;
  onEdit: (profile: RuntimeProfile) => void;
  onDeleted: () => void;
}) {
  const { t } = useT("runtimes");
  const [deleteOpen, setDeleteOpen] = useState(false);

  if (!entry) {
    return (
      <div className="flex min-h-[12rem] flex-1 items-center justify-center p-6 text-center">
        <p className="text-sm text-muted-foreground">
          {t(($) => $.profiles.detail.select_hint)}
        </p>
      </div>
    );
  }

  if (entry.kind === "builtin") {
    return (
      <div className="min-h-0 flex-1 overflow-y-auto p-6">
        <div className="flex items-center gap-3">
          <span className="flex h-10 w-10 items-center justify-center rounded-md border bg-background">
            <ProviderLogo provider={entry.protocolFamily} className="h-5 w-5" />
          </span>
          <div className="min-w-0">
            <h3 className="truncate text-base font-semibold capitalize">
              {entry.protocolFamily}
            </h3>
            <span className="text-xs text-muted-foreground">
              {t(($) => $.profiles.builtin_detail.read_only)}
            </span>
          </div>
        </div>
        <p className="mt-4 text-sm text-muted-foreground">
          {t(($) => $.profiles.builtin_detail.description, {
            family: entry.protocolFamily,
          })}
        </p>
      </div>
    );
  }

  const profile = entry.profile;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="min-h-0 flex-1 overflow-y-auto p-6">
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <span className="flex h-10 w-10 items-center justify-center rounded-md border bg-background">
              <ProviderLogo
                provider={profile.protocol_family}
                className="h-5 w-5"
              />
            </span>
            <div className="min-w-0">
              <h3 className="truncate text-base font-semibold">
                {profile.display_name}
              </h3>
              <span className="text-xs capitalize text-muted-foreground">
                {profile.protocol_family}
              </span>
            </div>
          </div>
        </div>

        <dl className="mt-5 space-y-4">
          <DetailRow label={t(($) => $.profiles.detail.base_family)}>
            <span className="capitalize">{profile.protocol_family}</span>
          </DetailRow>
          <DetailRow label={t(($) => $.profiles.detail.command)}>
            <span className="font-mono text-xs">{profile.command_name}</span>
          </DetailRow>
          <DetailRow label={t(($) => $.profiles.detail.description)}>
            {profile.description ? (
              <span>{profile.description}</span>
            ) : (
              <span className="text-muted-foreground">
                {t(($) => $.profiles.detail.no_description)}
              </span>
            )}
          </DetailRow>
          <DetailRow label={t(($) => $.profiles.detail.visibility)}>
            {profile.visibility === "private"
              ? t(($) => $.profiles.visibility.private)
              : t(($) => $.profiles.visibility.workspace)}
          </DetailRow>
        </dl>
      </div>

      <div className="flex shrink-0 justify-end gap-2 border-t bg-muted/30 px-6 py-3">
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => onEdit(profile)}
        >
          <Pencil className="h-3.5 w-3.5" />
          {t(($) => $.profiles.detail.edit)}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="text-destructive hover:text-destructive"
          onClick={() => setDeleteOpen(true)}
        >
          <Trash2 className="h-3.5 w-3.5" />
          {t(($) => $.profiles.detail.delete)}
        </Button>
      </div>

      <DeleteRuntimeProfileDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        profile={profile}
        wsId={wsId}
        onDeleted={() => {
          setDeleteOpen(false);
          onDeleted();
        }}
      />
    </div>
  );
}

function DetailRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <dt className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </dt>
      <dd className="mt-1 text-sm">{children}</dd>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Create / edit form.
// ---------------------------------------------------------------------------

function ProfileFormView({
  wsId,
  mode,
  step,
  family,
  profile,
  onPickFamily,
  onBack,
  onCancel,
  onSaved,
}: {
  wsId: string;
  mode: "create" | "edit";
  step: "family" | "details";
  family: RuntimeProtocolFamily;
  profile: RuntimeProfile | null;
  onPickFamily: (family: RuntimeProtocolFamily) => void;
  onBack: () => void;
  onCancel: () => void;
  onSaved: (profile: RuntimeProfile) => void;
}) {
  const { t } = useT("runtimes");

  if (mode === "create" && step === "family") {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
          <h3 className="text-sm font-medium">
            {t(($) => $.profiles.form.step_family_label)}
          </h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {t(($) => $.profiles.form.step_family_hint)}
          </p>
          <div
            className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-3"
            role="radiogroup"
            aria-label={t(($) => $.profiles.form.family_label)}
          >
            {PROTOCOL_FAMILIES.map((option) => (
              <button
                key={option}
                type="button"
                role="radio"
                aria-checked={option === family}
                onClick={() => onPickFamily(option)}
                className="flex items-center gap-2 rounded-md border bg-background px-3 py-2.5 text-left text-sm transition-colors hover:bg-accent/50 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
              >
                <ProviderLogo provider={option} className="h-4 w-4 shrink-0" />
                <span className="truncate capitalize">{option}</span>
              </button>
            ))}
          </div>
        </div>
        <div className="flex shrink-0 justify-between gap-2 border-t bg-muted/30 px-6 py-3">
          <Button type="button" variant="ghost" size="sm" onClick={onBack}>
            <ChevronLeft className="h-3.5 w-3.5" />
            {t(($) => $.profiles.form.back)}
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={onCancel}>
            {t(($) => $.profiles.form.cancel)}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <ProfileDetailsForm
      wsId={wsId}
      mode={mode}
      family={family}
      profile={profile}
      onBack={onBack}
      onCancel={onCancel}
      onSaved={onSaved}
    />
  );
}

function ProfileDetailsForm({
  wsId,
  mode,
  family,
  profile,
  onBack,
  onCancel,
  onSaved,
}: {
  wsId: string;
  mode: "create" | "edit";
  family: RuntimeProtocolFamily;
  profile: RuntimeProfile | null;
  onBack: () => void;
  onCancel: () => void;
  onSaved: (profile: RuntimeProfile) => void;
}) {
  const { t } = useT("runtimes");
  const idPrefix = `runtime-profile-${useId().replace(/:/g, "")}`;
  const createProfile = useCreateRuntimeProfile(wsId);
  const updateProfile = useUpdateRuntimeProfile(wsId);

  const [values, setValues] = useState<ProfileFormValues>({
    displayName: profile?.display_name ?? "",
    commandName: profile?.command_name ?? "",
    description: profile?.description ?? "",
  });
  const [visibility, setVisibility] = useState<RuntimeProfileVisibility>(
    profile?.visibility ?? "workspace",
  );
  const [errors, setErrors] = useState<ProfileFormErrorField[]>([]);
  // Server-side error surfaced under the display-name field (duplicate) or
  // as a generic banner.
  const [duplicateName, setDuplicateName] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const submitting = createProfile.isPending || updateProfile.isPending;
  const setField = (key: keyof ProfileFormValues, value: string) => {
    setValues((prev) => ({ ...prev, [key]: value }));
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setFormError(null);
    setDuplicateName(false);
    const validationErrors = validateProfileForm(values);
    setErrors(validationErrors);
    if (validationErrors.length > 0) return;

    const description = values.description.trim();

    try {
      if (mode === "create") {
        const created = await createProfile.mutateAsync({
          display_name: values.displayName.trim(),
          protocol_family: family,
          command_name: values.commandName.trim(),
          ...(description ? { description } : {}),
          visibility,
        });
        toast.success(t(($) => $.profiles.form.toast_created));
        onSaved(created);
      } else if (profile) {
        const updated = await updateProfile.mutateAsync({
          profileId: profile.id,
          patch: {
            display_name: values.displayName.trim(),
            command_name: values.commandName.trim(),
            description: description ? description : null,
            visibility,
          },
        });
        toast.success(t(($) => $.profiles.form.toast_updated));
        onSaved(updated);
      }
    } catch (err) {
      // 409 from create/patch means the display name collides.
      if (err instanceof ApiError && err.status === 409) {
        setDuplicateName(true);
        return;
      }
      setFormError(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.profiles.form.error_generic),
      );
    }
  };

  const formId = `${idPrefix}-form`;
  const hasError = (field: ProfileFormErrorField) => errors.includes(field);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <form
        id={formId}
        onSubmit={handleSubmit}
        className="min-h-0 flex-1 space-y-4 overflow-y-auto px-6 py-5"
      >
        <h3 className="text-sm font-medium">
          {mode === "create"
            ? t(($) => $.profiles.form.step_details_label)
            : t(($) => $.profiles.form.edit_title)}
        </h3>

        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">
            {t(($) => $.profiles.form.family_label)}
          </Label>
          <div className="flex items-center gap-2 rounded-md border bg-muted/30 px-3 py-2">
            <ProviderLogo provider={family} className="h-4 w-4 shrink-0" />
            <span className="text-sm capitalize">{family}</span>
          </div>
          <p className="text-[11px] text-muted-foreground">
            {t(($) => $.profiles.form.family_locked_hint)}
          </p>
        </div>

        <div className="space-y-1.5">
          <Label
            htmlFor={`${idPrefix}-display-name`}
            className="text-xs text-muted-foreground"
          >
            {t(($) => $.profiles.form.display_name_label)}
          </Label>
          <Input
            id={`${idPrefix}-display-name`}
            value={values.displayName}
            onChange={(e) => setField("displayName", e.target.value)}
            placeholder={t(($) => $.profiles.form.display_name_placeholder)}
            aria-invalid={hasError("displayName") || duplicateName}
            aria-describedby={
              hasError("displayName") || duplicateName
                ? `${idPrefix}-display-name-error`
                : undefined
            }
            className="h-9 text-sm"
          />
          {hasError("displayName") && (
            <p
              id={`${idPrefix}-display-name-error`}
              className="text-xs text-destructive"
            >
              {t(($) => $.profiles.form.error_display_name_required)}
            </p>
          )}
          {duplicateName && !hasError("displayName") && (
            <p
              id={`${idPrefix}-display-name-error`}
              className="text-xs text-destructive"
            >
              {t(($) => $.profiles.form.error_duplicate_name)}
            </p>
          )}
        </div>

        <div className="space-y-1.5">
          <Label
            htmlFor={`${idPrefix}-command`}
            className="text-xs text-muted-foreground"
          >
            {t(($) => $.profiles.form.command_name_label)}
          </Label>
          <Input
            id={`${idPrefix}-command`}
            value={values.commandName}
            onChange={(e) => setField("commandName", e.target.value)}
            placeholder={t(($) => $.profiles.form.command_name_placeholder)}
            aria-invalid={hasError("commandName")}
            aria-describedby={
              hasError("commandName") ? `${idPrefix}-command-error` : undefined
            }
            className="h-9 font-mono text-sm"
          />
          {hasError("commandName") && (
            <p id={`${idPrefix}-command-error`} className="text-xs text-destructive">
              {t(($) => $.profiles.form.error_command_required)}
            </p>
          )}
        </div>

        <div className="space-y-1.5">
          <Label
            htmlFor={`${idPrefix}-description`}
            className="text-xs text-muted-foreground"
          >
            {t(($) => $.profiles.form.description_label)}
          </Label>
          <Textarea
            id={`${idPrefix}-description`}
            value={values.description}
            onChange={(e) => setField("description", e.target.value)}
            placeholder={t(($) => $.profiles.form.description_placeholder)}
            className="min-h-16 text-sm"
          />
        </div>

        {/* NOTE: a `fixed_args` input is intentionally omitted in v1 — the
            daemon does not yet pass these args to the agent launch command, so
            exposing the field would promise admins a no-op. Re-add only once
            it's wired end-to-end. See TODO(MUL-3284) in
            server/internal/daemon/daemon.go. */}

        <fieldset className="space-y-1.5">
          <legend className="text-xs text-muted-foreground">
            {t(($) => $.profiles.form.visibility_label)}
          </legend>
          <div className="flex gap-2">
            <VisibilityOption
              active={visibility === "workspace"}
              label={t(($) => $.profiles.visibility.workspace)}
              hint={t(($) => $.profiles.form.visibility_workspace_hint)}
              onClick={() => setVisibility("workspace")}
            />
            <VisibilityOption
              active={visibility === "private"}
              label={t(($) => $.profiles.visibility.private)}
              hint={t(($) => $.profiles.form.visibility_private_hint)}
              onClick={() => setVisibility("private")}
            />
          </div>
        </fieldset>

        {formError && (
          <p role="alert" className="text-xs text-destructive">
            {formError}
          </p>
        )}
      </form>

      <div className="flex shrink-0 justify-between gap-2 border-t bg-muted/30 px-6 py-3">
        <Button type="button" variant="ghost" size="sm" onClick={onBack}>
          <ChevronLeft className="h-3.5 w-3.5" />
          {t(($) => $.profiles.form.back)}
        </Button>
        <div className="flex gap-2">
          <Button type="button" variant="outline" size="sm" onClick={onCancel}>
            {t(($) => $.profiles.form.cancel)}
          </Button>
          <Button type="submit" size="sm" form={formId} disabled={submitting}>
            {submitting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            {mode === "create"
              ? submitting
                ? t(($) => $.profiles.form.creating)
                : t(($) => $.profiles.form.create)
              : submitting
                ? t(($) => $.profiles.form.saving)
                : t(($) => $.profiles.form.save)}
          </Button>
        </div>
      </div>
    </div>
  );
}

function VisibilityOption({
  active,
  label,
  hint,
  onClick,
}: {
  active: boolean;
  label: string;
  hint: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      onClick={onClick}
      title={hint}
      className={cn(
        "flex-1 rounded-md border px-3 py-2 text-left text-sm transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
        active ? "border-primary bg-accent" : "bg-background hover:bg-accent/50",
      )}
    >
      <span className="font-medium">{label}</span>
      <span className="mt-0.5 block text-[11px] text-muted-foreground">
        {hint}
      </span>
    </button>
  );
}
