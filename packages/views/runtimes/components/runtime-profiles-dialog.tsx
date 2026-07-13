"use client";

import { useId, useMemo, useState } from "react";
import type { FormEvent } from "react";
import {
  ChevronDown,
  ChevronLeft,
  ExternalLink,
  Loader2,
  Pencil,
  Plus,
  Server,
  Trash2,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type {
  RuntimeProfile,
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
  DialogClose,
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
  formatCommandLine,
  parseCommandLine,
  validateProfileForm,
  type ProfileFormErrorField,
  type ProfileFormValues,
  type RuntimeCatalogEntry,
  type RuntimeCatalogSections,
} from "./runtime-profile-catalog";
import { useT } from "../../i18n";
import { customRuntimeDocsHref } from "./runtime-docs";

// The dialog runs in two surfaces that swap inside one Popup:
//   - "browse": custom-first master list + adaptive detail
//   - "form":   create (2-step) or edit (single step, family locked)
type DialogState =
  | { surface: "browse" }
  | { surface: "form"; mode: "create"; step: "family" | "details" }
  | { surface: "form"; mode: "edit"; profile: RuntimeProfile };

export function RuntimeProfilesDialog({
  wsId,
  intent = "manage",
  machineName,
  initialProfile,
  onProfileCreated,
  onClose,
}: {
  wsId: string;
  intent?: "create" | "edit" | "manage";
  machineName?: string;
  initialProfile?: RuntimeProfile;
  onProfileCreated?: (profile: RuntimeProfile) => void;
  onClose: () => void;
}) {
  const { t, i18n } = useT("runtimes");
  const { data: profiles = [], isLoading } = useQuery(
    runtimeProfileListOptions(wsId),
  );

  const [state, setState] = useState<DialogState>(() => {
    if (intent === "create") {
      return { surface: "form", mode: "create", step: "family" };
    }
    if (intent === "edit" && initialProfile) {
      return { surface: "form", mode: "edit", profile: initialProfile };
    }
    return { surface: "browse" };
  });
  const [selectedId, setSelectedId] = useState<string | null>(null);
  // Carries the chosen family from create-step-1 into the form.
  const [draftFamily, setDraftFamily] =
    useState<RuntimeProtocolFamily>(PROTOCOL_FAMILIES[0] ?? "claude");

  const catalog = useMemo(() => buildRuntimeCatalog(profiles), [profiles]);
  const entries = useMemo(
    () => [...catalog.customs, ...catalog.builtins],
    [catalog],
  );
  const selectedEntry =
    entries.find((entry) => entry.id === selectedId) ?? null;
  const openCreateForm = () =>
    setState({ surface: "form", mode: "create", step: "family" });
  const docsHref = customRuntimeDocsHref(i18n.language);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        className={cn(
          "flex max-h-[88vh] flex-col gap-0 overscroll-contain p-0",
          intent === "manage" ? "sm:max-w-3xl" : "sm:max-w-2xl",
        )}
        showCloseButton={false}
      >
        <DialogHeader className="border-b px-6 py-5">
          <div className="flex items-start justify-between gap-3">
            <DialogTitle className="flex min-w-0 items-center gap-2 text-base">
              <Server
                aria-hidden="true"
                className="h-4 w-4 shrink-0 text-muted-foreground"
              />
              <span className="truncate">
                {intent === "create"
                  ? t(($) => $.profiles.form.create_title)
                  : intent === "edit"
                    ? t(($) => $.profiles.form.edit_title)
                    : t(($) => $.profiles.dialog_title)}
              </span>
            </DialogTitle>
            <div className="flex shrink-0 items-center gap-2">
              {state.surface === "browse" && catalog.customs.length > 0 && (
                <Button
                  type="button"
                  size="sm"
                  className="h-8 px-2.5"
                  onClick={openCreateForm}
                >
                  <Plus aria-hidden="true" className="h-3.5 w-3.5" />
                  {t(($) => $.profiles.add_new)}
                </Button>
              )}
              <DialogClose
                render={
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    className="shrink-0"
                    aria-label={t(($) => $.profiles.close)}
                  />
                }
              >
                <X aria-hidden="true" className="h-4 w-4" />
                <span className="sr-only">{t(($) => $.profiles.close)}</span>
              </DialogClose>
            </div>
          </div>
          <DialogDescription className="text-xs">
            <span className="block">
              {intent === "create"
                ? t(($) => $.profiles.create_dialog_description, {
                    machine: machineName ?? t(($) => $.profiles.this_machine),
                  })
                : t(($) => $.profiles.dialog_description)}
            </span>{" "}
            <a
              href={docsHref}
              target="_blank"
              rel="noopener noreferrer"
              className="mt-1 inline-flex items-center gap-1 rounded-sm font-medium text-foreground underline underline-offset-2 transition-colors hover:text-foreground/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {t(($) => $.profiles.learn_more)}
              <ExternalLink aria-hidden="true" className="h-3 w-3" />
            </a>
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
            standaloneCreate={intent === "create"}
            standaloneEdit={intent === "edit"}
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
            onCancel={() => {
              if (intent !== "manage") {
                onClose();
              } else {
                setState({ surface: "browse" });
              }
            }}
            onSaved={(profile) => {
              if (state.surface === "form" && state.mode === "create") {
                onProfileCreated?.(profile);
              }
              if (intent !== "manage") {
                onClose();
                return;
              }
              setSelectedId(profile.id);
              setState({ surface: "browse" });
            }}
          />
        ) : (
          <div className="grid min-h-0 flex-1 grid-cols-1 md:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
            <CatalogList
              catalog={catalog}
              loading={isLoading}
              selectedId={selectedId}
              onSelect={setSelectedId}
              onAddNew={openCreateForm}
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
// Master list — custom profiles first, built-in families as collapsed reference.
// ---------------------------------------------------------------------------

function CatalogList({
  catalog,
  loading,
  selectedId,
  onSelect,
  onAddNew,
}: {
  catalog: RuntimeCatalogSections;
  loading: boolean;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  onAddNew: () => void;
}) {
  const { t } = useT("runtimes");
  const [builtinsOpen, setBuiltinsOpen] = useState(false);
  const hasCustom = catalog.customs.length > 0;
  const selectedIsBuiltin = catalog.builtins.some(
    (entry) => entry.id === selectedId,
  );

  return (
    <div className="flex min-h-0 flex-col border-b md:border-b-0 md:border-r">
      <div className="flex shrink-0 items-center justify-between border-b bg-background px-4 py-3">
        <h3 className="text-sm font-medium">
          {t(($) => $.profiles.list_title)}
        </h3>
      </div>
      {loading ? (
        <div className="flex h-40 items-center justify-center">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      ) : (
        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
          <section aria-labelledby="runtime-profile-custom-section">
            <div className="mb-2 flex items-center justify-between gap-3">
              <h4
                id="runtime-profile-custom-section"
                className="text-xs font-medium uppercase tracking-wide text-muted-foreground"
              >
                {t(($) => $.profiles.custom_section_title, {
                  count: catalog.customs.length,
                })}
              </h4>
            </div>
            {hasCustom ? (
              <ul
                className="space-y-1"
                role="listbox"
                aria-label={t(($) => $.profiles.custom_section_aria)}
              >
                {catalog.customs.map((entry) => (
                  <li key={entry.id}>
                    <CatalogRow
                      entry={entry}
                      active={entry.id === selectedId}
                      onClick={() => onSelect(entry.id)}
                    />
                  </li>
                ))}
              </ul>
            ) : (
              <EmptyCustomState onAddNew={onAddNew} />
            )}
          </section>

          <section aria-labelledby="runtime-profile-builtin-section">
            <button
              type="button"
              className="flex w-full items-center justify-between gap-3 rounded-md border bg-muted/30 px-3 py-2.5 text-left transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              aria-expanded={builtinsOpen}
              aria-controls="runtime-profile-builtin-list"
              onClick={() =>
                setBuiltinsOpen((open) => {
                  const nextOpen = !open;
                  if (!nextOpen && selectedIsBuiltin) {
                    onSelect(null);
                  }
                  return nextOpen;
                })
              }
            >
              <span className="min-w-0">
                <span
                  id="runtime-profile-builtin-section"
                  className="block text-xs font-medium uppercase tracking-wide text-muted-foreground"
                >
                  {t(($) => $.profiles.builtin_section_title, {
                    count: catalog.builtins.length,
                  })}
                </span>
                <span className="mt-0.5 block truncate text-xs text-muted-foreground">
                  {t(($) => $.profiles.builtin_section_hint)}
                </span>
              </span>
              <ChevronDown
                className={cn(
                  "h-4 w-4 shrink-0 text-muted-foreground transition-transform",
                  builtinsOpen && "rotate-180",
                )}
              />
            </button>
            {builtinsOpen && (
              <ul
                id="runtime-profile-builtin-list"
                className="mt-2 space-y-1"
                role="listbox"
                aria-label={t(($) => $.profiles.builtin_section_aria)}
              >
                {catalog.builtins.map((entry) => (
                  <li key={entry.id}>
                    <CatalogRow
                      entry={entry}
                      active={entry.id === selectedId}
                      onClick={() => onSelect(entry.id)}
                    />
                  </li>
                ))}
              </ul>
            )}
          </section>
        </div>
      )}
    </div>
  );
}

function EmptyCustomState({ onAddNew }: { onAddNew: () => void }) {
  const { t } = useT("runtimes");
  return (
    <div className="rounded-md border bg-muted/20 p-4">
      <div className="flex items-start gap-3">
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-background">
          <Server aria-hidden="true" className="h-4 w-4 text-muted-foreground" />
        </span>
        <div className="min-w-0 flex-1">
          <h5 className="text-sm font-medium">{t(($) => $.profiles.empty_title)}</h5>
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            {t(($) => $.profiles.empty_description)}
          </p>
          <Button
            type="button"
            size="sm"
            className="mt-3 h-8 px-2.5"
            onClick={onAddNew}
          >
            <Plus aria-hidden="true" className="h-3.5 w-3.5" />
            {t(($) => $.profiles.add_new)}
          </Button>
        </div>
      </div>
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
  const isBuiltin = entry.kind === "builtin";
  return (
    <button
      type="button"
      role="option"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "flex w-full min-w-0 items-center gap-2.5 rounded-md px-3 py-2 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        active && "bg-accent",
        !active && !isBuiltin && "hover:bg-accent/50",
        isBuiltin && !active && "text-muted-foreground hover:bg-muted/40",
      )}
    >
      <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md border bg-background">
        <ProviderLogo
          provider={entry.protocolFamily}
          className={cn("h-4 w-4", isBuiltin && "opacity-75")}
        />
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
      {isBuiltin && (
        <span className="shrink-0 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
          {t(($) => $.profiles.builtin_reference)}
        </span>
      )}
    </button>
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
      <div className="flex min-h-[12rem] flex-1 items-center justify-center p-6">
        <div className="max-w-sm text-center">
          <span className="mx-auto flex h-11 w-11 items-center justify-center rounded-md border bg-background">
            <Server className="h-5 w-5 text-muted-foreground" />
          </span>
          <h3 className="mt-4 text-base font-semibold">
            {t(($) => $.profiles.detail.default_title)}
          </h3>
          <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
            {t(($) => $.profiles.detail.default_description)}
          </p>
        </div>
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
  const commandLine = formatCommandLine(
    profile.command_name,
    profile.fixed_args,
  );

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
            <span className="font-mono text-xs">{commandLine}</span>
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
  standaloneCreate,
  standaloneEdit,
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
  standaloneCreate: boolean;
  standaloneEdit: boolean;
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
          <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            {t(($) => $.profiles.form.step_progress, { current: 1, total: 2 })}
          </p>
          <h3 className="text-sm font-medium">
            {t(($) => $.profiles.form.step_family_label)}
          </h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {t(($) => $.profiles.form.step_family_hint)}
          </p>
          <div
            className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-3"
            aria-label={t(($) => $.profiles.form.family_label)}
          >
            {PROTOCOL_FAMILIES.map((option) => (
              <button
                key={option}
                type="button"
                onClick={() => onPickFamily(option)}
                className="flex items-center gap-2 rounded-md border bg-background px-3 py-2.5 text-left text-sm transition-colors hover:bg-accent/50 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
              >
                <ProviderLogo provider={option} className="h-4 w-4 shrink-0" />
                <span className="truncate capitalize">{option}</span>
              </button>
            ))}
          </div>
        </div>
        <div
          className={cn(
            "flex shrink-0 gap-2 border-t bg-muted/30 px-6 py-3",
            standaloneCreate ? "justify-end" : "justify-start",
          )}
        >
          {standaloneCreate ? (
            <Button type="button" variant="outline" size="sm" onClick={onCancel}>
              {t(($) => $.profiles.form.cancel)}
            </Button>
          ) : (
            <Button type="button" variant="ghost" size="sm" onClick={onBack}>
              <ChevronLeft aria-hidden="true" className="h-3.5 w-3.5" />
              {t(($) => $.profiles.form.back)}
            </Button>
          )}
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
      hideEditHeading={standaloneEdit}
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
  hideEditHeading,
  onBack,
  onCancel,
  onSaved,
}: {
  wsId: string;
  mode: "create" | "edit";
  family: RuntimeProtocolFamily;
  profile: RuntimeProfile | null;
  hideEditHeading: boolean;
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
    commandLine: profile
      ? formatCommandLine(profile.command_name, profile.fixed_args)
      : "",
    description: profile?.description ?? "",
  });
  const [errors, setErrors] = useState<ProfileFormErrorField[]>([]);
  // Server-side error surfaced under the display-name field (duplicate) or
  // as a generic banner.
  const [duplicateName, setDuplicateName] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const submitting = createProfile.isPending || updateProfile.isPending;
  const setField = (key: keyof ProfileFormValues, value: string) => {
    setValues((prev) => ({ ...prev, [key]: value }));
  };

  const parsedCommand = useMemo(
    () => parseCommandLine(values.commandLine),
    [values.commandLine],
  );

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setFormError(null);
    setDuplicateName(false);
    const validationErrors = validateProfileForm(values);
    if (!validationErrors.includes("commandLine") && !parsedCommand.ok) {
      validationErrors.push("commandLine");
    }
    setErrors(validationErrors);
    if (validationErrors.length > 0) return;
    if (!parsedCommand.ok) return;

    const description = values.description.trim();
    const commandName = parsedCommand.commandName;
    const fixedArgs = parsedCommand.fixedArgs;

    try {
      if (mode === "create") {
        const created = await createProfile.mutateAsync({
          display_name: values.displayName.trim(),
          protocol_family: family,
          command_name: commandName,
          fixed_args: fixedArgs,
          ...(description ? { description } : {}),
        });
        toast.success(t(($) => $.profiles.form.toast_created));
        onSaved(created);
      } else if (profile) {
        const updated = await updateProfile.mutateAsync({
          profileId: profile.id,
          patch: {
            display_name: values.displayName.trim(),
            command_name: commandName,
            fixed_args: fixedArgs,
            description: description ? description : null,
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
  const parseErrorMessage =
    !parsedCommand.ok && parsedCommand.error === "unclosed_quote"
      ? t(($) => $.profiles.form.error_command_unclosed_quote)
      : !parsedCommand.ok && parsedCommand.error === "trailing_escape"
        ? t(($) => $.profiles.form.error_command_trailing_escape)
      : !parsedCommand.ok && parsedCommand.error === "shell_expansion"
        ? t(($) => $.profiles.form.error_command_shell_expansion)
        : !parsedCommand.ok && parsedCommand.error === "shell_syntax"
          ? t(($) => $.profiles.form.error_command_shell_syntax)
          : null;
  const commandError =
    hasError("commandLine") && !values.commandLine.trim()
      ? t(($) => $.profiles.form.error_command_required)
      : hasError("commandLine") && !parsedCommand.ok
        ? (parseErrorMessage ?? t(($) => $.profiles.form.error_command_required))
        : null;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <form
        id={formId}
        onSubmit={handleSubmit}
        className="min-h-0 flex-1 space-y-4 overflow-y-auto px-6 py-5"
      >
        {mode === "create" && (
          <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            {t(($) => $.profiles.form.step_progress, { current: 2, total: 2 })}
          </p>
        )}
        {(mode === "create" || !hideEditHeading) && (
          <h3 className="text-sm font-medium">
            {mode === "create"
              ? t(($) => $.profiles.form.step_details_label)
              : t(($) => $.profiles.form.edit_title)}
          </h3>
        )}

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
            name="displayName"
            autoComplete="off"
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
            name="command"
            autoComplete="off"
            spellCheck={false}
            value={values.commandLine}
            onChange={(e) => setField("commandLine", e.target.value)}
            placeholder={t(($) => $.profiles.form.command_name_placeholder)}
            aria-invalid={hasError("commandLine")}
            aria-describedby={
              hasError("commandLine") ? `${idPrefix}-command-error` : undefined
            }
            className="h-9 font-mono text-sm"
          />
          {commandError && (
            <p id={`${idPrefix}-command-error`} className="text-xs text-destructive">
              {commandError}
            </p>
          )}
          {parsedCommand.ok && (
            <div className="space-y-1 rounded-md border bg-muted/20 px-3 py-2 text-[11px] text-muted-foreground">
              <div className="flex min-w-0 gap-1">
                <span>{t(($) => $.profiles.form.command_preview_executable)}</span>
                <span className="truncate font-mono text-foreground">
                  {parsedCommand.commandName}
                </span>
              </div>
              {parsedCommand.fixedArgs.length > 0 && (
                <div className="flex min-w-0 flex-wrap gap-1">
                  <span>{t(($) => $.profiles.form.command_preview_args)}</span>
                  {parsedCommand.fixedArgs.map((arg, index) => (
                    <span
                      key={`${arg}-${index}`}
                      className="rounded bg-background px-1 font-mono text-foreground"
                    >
                      {arg}
                    </span>
                  ))}
                </div>
              )}
            </div>
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
            name="description"
            autoComplete="off"
            value={values.description}
            onChange={(e) => setField("description", e.target.value)}
            placeholder={t(($) => $.profiles.form.description_placeholder)}
            className="min-h-16 text-sm"
          />
        </div>

        {/* NOTE: a visibility control is intentionally omitted in v1. The
            server forces every profile to 'workspace' because the read paths
            (list, daemon pull, register) do not yet enforce 'private', so
            offering a private toggle would leak the profile to other members.
            Re-add once creator-visibility filtering exists. Follow-up:
            MUL-3308. */}

        {formError && (
          <p role="alert" className="text-xs text-destructive">
            {formError}
          </p>
        )}
      </form>

      <div className="flex shrink-0 justify-between gap-2 border-t bg-muted/30 px-6 py-3">
        {mode === "create" ? (
          <Button type="button" variant="ghost" size="sm" onClick={onBack}>
            <ChevronLeft aria-hidden="true" className="h-3.5 w-3.5" />
            {t(($) => $.profiles.form.back)}
          </Button>
        ) : (
          <span />
        )}
        <div className="flex gap-2">
          <Button type="button" variant="outline" size="sm" onClick={onCancel}>
            {t(($) => $.profiles.form.cancel)}
          </Button>
          <Button type="submit" size="sm" form={formId} disabled={submitting}>
            {submitting && (
              <Loader2 aria-hidden="true" className="h-3.5 w-3.5 animate-spin" />
            )}
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
