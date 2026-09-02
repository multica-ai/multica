"use client";

import { useId, useState, type ReactNode } from "react";
import { ChevronRight, Trash2 } from "lucide-react";
import { ApiError } from "@multica/core/api";
import { useCurrentMember } from "@multica/core/permissions";
import {
  useModelPricing,
  useRefreshModelPricing,
  useSaveModelPricing,
} from "@multica/core/runtimes/pricing-queries";
import {
  clearImportedModelPrices,
  readLegacyModelPrices,
  resolveModelPricing,
  type ModelPricingRow,
  type ModelPricingSnapshot,
} from "@multica/core/runtimes/pricing";
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
import { useT } from "../../i18n";
import {
  formatReferenceRate,
  hasPriceChanges,
  parsePriceDrafts,
  previewLegacyPrices,
  toPriceDraft,
  type PriceDraft,
} from "../pricing-drafts";

interface Props {
  wsId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  unmappedModels: readonly string[];
}

interface PriceSession {
  revision: number;
  initialOverrides: Record<string, ModelPricingRow>;
  drafts: Record<string, PriceDraft>;
  imported: Record<string, ModelPricingRow>;
  importPreview: boolean;
}

function startPriceSession(snapshot: ModelPricingSnapshot): PriceSession {
  return {
    revision: snapshot.revision,
    initialOverrides: snapshot.overrides,
    drafts: Object.fromEntries(
      Object.entries(snapshot.overrides).map(([key, row]) => [
        key,
        toPriceDraft(row),
      ]),
    ),
    imported: {},
    importPreview: false,
  };
}

export function CustomPricingDialog({
  wsId,
  open,
  onOpenChange,
  unmappedModels,
}: Props) {
  const { t } = useT("runtimes");
  const query = useModelPricing(wsId);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.usage.custom_pricing.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.usage.custom_pricing.description)}
          </DialogDescription>
        </DialogHeader>
        {open && query.data ? (
          <PricingEditor
            key={wsId}
            wsId={wsId}
            snapshot={query.data}
            unmappedModels={unmappedModels}
            onClose={() => onOpenChange(false)}
            onReload={async () => {
              const result = await query.refetch();
              return result.isError ? undefined : result.data;
            }}
          />
        ) : (
          <>
            <div role="status" className="text-caption text-muted-foreground">
              {query.isError
                ? t(($) => $.usage.custom_pricing.load_error)
                : t(($) => $.usage.custom_pricing.loading)}
              {query.isError && (
                <Button variant="ghost" onClick={() => void query.refetch()}>
                  {t(($) => $.usage.custom_pricing.retry)}
                </Button>
              )}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                {t(($) => $.usage.custom_pricing.close)}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

function PricingEditor({
  wsId,
  snapshot,
  unmappedModels,
  onClose,
  onReload,
}: {
  wsId: string;
  snapshot: ModelPricingSnapshot;
  unmappedModels: readonly string[];
  onClose: () => void;
  onReload: () => Promise<ModelPricingSnapshot | undefined>;
}) {
  const { t, i18n } = useT("runtimes");
  const { role } = useCurrentMember(wsId);
  const canManage =
    snapshot.canManage === true && (role === "owner" || role === "admin");
  const save = useSaveModelPricing(wsId);
  const refresh = useRefreshModelPricing(wsId);
  // Browsing uses the current query result. An edit captures its own revision
  // and original prices, so background refreshes cannot replace the draft.
  const [session, setSession] = useState<PriceSession | null>(null);
  const editing = canManage && session !== null;
  const [model, setModel] = useState(unmappedModels[0] ?? "");
  const selectedModel = model.trim().toLowerCase();
  const [legacy, setLegacy] = useState(readLegacyModelPrices);
  const [reloading, setReloading] = useState(false);
  const [reloadFailed, setReloadFailed] = useState(false);
  const listId = useId();
  const busy = save.isPending || refresh.isPending || reloading;
  const conflict = save.error instanceof ApiError && save.error.status === 409;
  const fields = [
    ["input", t(($) => $.usage.custom_pricing.field_input)],
    ["output", t(($) => $.usage.custom_pricing.field_output)],
    ["cacheRead", t(($) => $.usage.custom_pricing.field_cache_read)],
    ["cacheWrite", t(($) => $.usage.custom_pricing.field_cache_write)],
  ] as const;
  const reference = selectedModel
    ? resolveModelPricing(selectedModel, undefined, {
        ...snapshot,
        overrides: {},
      })
    : undefined;
  const effective = selectedModel
    ? resolveModelPricing(selectedModel, undefined, snapshot)
    : undefined;
  const selectedOverrideKey = Object.keys(snapshot.overrides).find(
    (key) => snapshot.overrides[key] === effective,
  );
  const selectedIsEdited =
    editing &&
    (selectedModel in session.drafts ||
      (selectedOverrideKey !== undefined &&
        selectedOverrideKey in session.initialOverrides));
  const overrides = session ? parsePriceDrafts(session.drafts) : null;
  const changed =
    session && overrides
      ? hasPriceChanges(session.initialOverrides, overrides)
      : false;
  const updatedAt = snapshot.succeededAt
    ? new Date(snapshot.succeededAt)
    : null;
  const validUpdate =
    updatedAt !== null && Number.isFinite(updatedAt.getTime());
  const shortUpdate = validUpdate
    ? updatedAt.toLocaleDateString(i18n.language, {
        month: "short",
        day: "numeric",
      })
    : t(($) => $.usage.custom_pricing.bundled);
  const fullUpdate = validUpdate
    ? updatedAt.toLocaleString(i18n.language, {
        dateStyle: "medium",
        timeStyle: "long",
      })
    : t(($) => $.usage.custom_pricing.bundled);

  const beginEditing = (key?: string) => {
    setReloadFailed(false);
    save.reset();
    setSession((previous) => {
      const next = previous ?? startPriceSession(snapshot);
      if (!key || key in next.drafts) return next;
      return {
        ...next,
        drafts: {
          ...next.drafts,
          [key]: toPriceDraft(resolveModelPricing(key, undefined, snapshot)),
        },
      };
    });
  };
  const cancelEditing = () => {
    setSession(null);
    setReloadFailed(false);
    save.reset();
  };
  const handleSave = async () => {
    if (!editing || !session || !overrides || !changed || busy || conflict)
      return;
    try {
      await save.mutateAsync({ revision: session.revision, overrides });
      clearImportedModelPrices(
        Object.fromEntries(
          Object.entries(session.imported).filter(([key]) => key in overrides),
        ),
      );
      setLegacy(readLegacyModelPrices());
      setSession(null);
    } catch {
      // Errors are rendered below; edits and their captured revision stay intact.
    }
  };
  const previewImport = () => {
    setSession((previous) => {
      const next = previous ?? startPriceSession(snapshot);
      const additions = previewLegacyPrices(
        legacy,
        snapshot.overrides,
        next.drafts,
      );
      return {
        ...next,
        imported: additions,
        importPreview: true,
        drafts: {
          ...next.drafts,
          ...Object.fromEntries(
            Object.entries(additions).map(([key, row]) => [
              key,
              toPriceDraft(row),
            ]),
          ),
        },
      };
    });
  };
  const reload = async () => {
    setReloading(true);
    setReloadFailed(false);
    try {
      const latest = await onReload();
      if (!latest) {
        setReloadFailed(true);
        return;
      }
      setSession(startPriceSession(latest));
      save.reset();
    } finally {
      setReloading(false);
    }
  };

  return (
    <>
      <div className="max-h-[60vh] space-y-4 overflow-y-auto">
        <Input
          value={model}
          list={listId}
          maxLength={512}
          aria-label={t(($) => $.usage.custom_pricing.model)}
          placeholder={t(($) => $.usage.custom_pricing.model)}
          onChange={(event) => setModel(event.target.value)}
        />
        <datalist id={listId}>
          {[
            ...new Set([
              ...unmappedModels,
              ...Object.keys(snapshot.rows),
              ...Object.keys(snapshot.aliases),
              ...Object.keys(snapshot.overrides),
            ]),
          ]
            .filter((key) => key.includes(selectedModel))
            .sort()
            .slice(0, 100)
            .map((key) => (
              <option key={key} value={key} />
            ))}
        </datalist>
        {selectedModel && !selectedIsEdited && (
          <PricePreview
            model={selectedModel}
            row={effective}
            custom={effective !== undefined && effective !== reference}
            action={
              canManage ? (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy || conflict}
                  onClick={() =>
                    beginEditing(
                      selectedOverrideKey === undefined
                        ? selectedModel
                        : undefined,
                    )
                  }
                >
                  {selectedOverrideKey === undefined
                    ? t(($) => $.usage.custom_pricing.add)
                    : t(($) => $.usage.custom_pricing.edit)}
                </Button>
              ) : undefined
            }
          />
        )}
        {!editing &&
          !selectedModel &&
          (Object.keys(snapshot.overrides).length > 0 ? (
            <div className="space-y-3">
              {canManage && (
                <div className="flex justify-end">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={busy}
                    onClick={() => beginEditing()}
                  >
                    {t(($) => $.usage.custom_pricing.edit)}
                  </Button>
                </div>
              )}
              {Object.entries(snapshot.overrides).map(([key, row]) => (
                <PricePreview key={key} model={key} row={row} custom />
              ))}
            </div>
          ) : (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.usage.custom_pricing.empty)}
            </p>
          ))}
        {editing && (
          <div className="space-y-3">
            <p className="text-caption text-muted-foreground">
              {t(($) => $.usage.custom_pricing.edit_hint)}
            </p>
            {Object.entries(session.drafts).map(([key, draft]) => (
              <div key={key} className="space-y-3 rounded-md border p-3">
                <div className="flex items-center justify-between gap-2">
                  <code className="break-all font-mono text-caption">
                    {key}
                  </code>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    disabled={busy}
                    aria-label={t(($) => $.usage.custom_pricing.remove_aria)}
                    onClick={() =>
                      setSession((previous) => {
                        if (!previous) return previous;
                        const drafts = { ...previous.drafts };
                        delete drafts[key];
                        return { ...previous, drafts };
                      })
                    }
                  >
                    <Trash2 />
                  </Button>
                </div>
                <p className="text-micro text-muted-foreground">
                  {t(($) => $.usage.custom_pricing.unit_hint)}
                </p>
                <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                  {fields.map(([field, label]) => (
                    <PriceField
                      key={field}
                      label={label}
                      value={draft[field]}
                      disabled={busy}
                      onChange={(value) =>
                        setSession((previous) =>
                          previous
                            ? {
                                ...previous,
                                drafts: {
                                  ...previous.drafts,
                                  [key]: { ...draft, [field]: value },
                                },
                              }
                            : previous,
                        )
                      }
                    />
                  ))}
                </div>
              </div>
            ))}
            {overrides === null && (
              <p role="alert" className="text-caption text-destructive">
                {t(($) => $.usage.custom_pricing.invalid)}
              </p>
            )}
            {session.importPreview && (
              <p role="status" className="text-caption text-muted-foreground">
                {Object.keys(session.imported).length > 0
                  ? t(($) => $.usage.custom_pricing.import_preview, {
                      count: Object.keys(session.imported).length,
                    })
                  : t(($) => $.usage.custom_pricing.import_empty)}
              </p>
            )}
            {save.isError && (
              <p role="alert" className="text-caption text-destructive">
                {conflict
                  ? t(($) => $.usage.custom_pricing.conflict)
                  : t(($) => $.usage.custom_pricing.save_error)}
              </p>
            )}
            {conflict && (
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => void reload()}
              >
                {t(($) => $.usage.custom_pricing.reload)}
              </Button>
            )}
            {reloadFailed && (
              <p role="alert" className="text-caption text-destructive">
                {t(($) => $.usage.custom_pricing.load_error)}
              </p>
            )}
          </div>
        )}
        {canManage && Object.keys(legacy).length > 0 && (
          <Button
            variant="outline"
            size="sm"
            disabled={busy || conflict || session?.importPreview === true}
            onClick={previewImport}
          >
            {t(($) => $.usage.custom_pricing.import_local)}
          </Button>
        )}
        <details className="group">
          <summary className="flex cursor-pointer list-none items-center gap-2 rounded-sm py-1 text-caption text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            <ChevronRight
              className="h-3 w-3 shrink-0 transition-transform group-open:rotate-90"
              aria-hidden
            />
            <span>{t(($) => $.usage.custom_pricing.details)}</span>
            <span className="ml-auto text-micro">
              {validUpdate
                ? t(($) => $.usage.custom_pricing.updated, {
                    time: shortUpdate,
                  })
                : shortUpdate}
            </span>
          </summary>
          <div className="space-y-2 pt-2 text-micro text-muted-foreground">
            <p>
              {t(($) => $.usage.custom_pricing.schedule, {
                timezone: snapshot.timezone,
              })}
            </p>
            <p>
              {t(($) => $.usage.custom_pricing.last_update, {
                time: fullUpdate,
              })}
            </p>
            {(snapshot.lastError || refresh.isError) && (
              <p role="status" className="text-warning">
                {t(($) => $.usage.custom_pricing.refresh_error)}
              </p>
            )}
            {!canManage && <p>{t(($) => $.usage.custom_pricing.readonly)}</p>}
            <div className="flex items-center justify-between gap-2">
              <a
                href="https://multica.ai/docs/developers/model-pricing"
                target="_blank"
                rel="noopener noreferrer"
                className="underline underline-offset-2"
              >
                {t(($) => $.usage.custom_pricing.learn_more)}
              </a>
              {canManage && (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  onClick={() => refresh.mutate()}
                >
                  {t(($) => $.usage.custom_pricing.refresh)}
                </Button>
              )}
            </div>
          </div>
        </details>
      </div>
      <DialogFooter>
        {editing ? (
          <>
            <Button
              variant="outline"
              disabled={save.isPending}
              onClick={cancelEditing}
            >
              {t(($) => $.usage.custom_pricing.cancel)}
            </Button>
            <Button
              disabled={busy || conflict || !changed || overrides === null}
              onClick={() => void handleSave()}
            >
              {t(($) => $.usage.custom_pricing.save)}
            </Button>
          </>
        ) : (
          <Button variant="outline" onClick={onClose}>
            {t(($) => $.usage.custom_pricing.close)}
          </Button>
        )}
      </DialogFooter>
    </>
  );
}

function PricePreview({
  model,
  row,
  custom,
  action,
}: {
  model: string;
  row?: ModelPricingRow;
  custom: boolean;
  action?: ReactNode;
}) {
  const { t } = useT("runtimes");
  const sourceLabels: Record<string, string> = {
    litellm: "LiteLLM",
    "models.dev": "Models.dev",
    bundled: t(($) => $.usage.custom_pricing.bundled),
  };
  const source = row?.source ?? "bundled";
  const fields = [
    ["input", t(($) => $.usage.custom_pricing.field_input)],
    ["output", t(($) => $.usage.custom_pricing.field_output)],
    ["cacheRead", t(($) => $.usage.custom_pricing.field_cache_read)],
    ["cacheWrite", t(($) => $.usage.custom_pricing.field_cache_write)],
  ] as const;
  return (
    <section
      className="space-y-3 rounded-md bg-muted/40 p-3"
      aria-label={model}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <code className="break-all font-mono text-caption">{model}</code>
        {action}
      </div>
      {row ? (
        <>
          <div className="flex flex-wrap justify-between gap-x-3 gap-y-1 text-micro text-muted-foreground">
            <p>
              {custom
                ? t(($) => $.usage.custom_pricing.override)
                : t(($) => $.usage.custom_pricing.source, {
                    source: sourceLabels[source] ?? source,
                  })}
              {row.sourceUrl && /^https?:\/\//.test(row.sourceUrl) && (
                <>
                  {" · "}
                  <a
                    className="underline underline-offset-2"
                    href={row.sourceUrl}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {t(($) => $.usage.custom_pricing.source_link)}
                  </a>
                </>
              )}
            </p>
            <span>{t(($) => $.usage.custom_pricing.unit_hint)}</span>
          </div>
          <dl className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            {fields.map(([field, label]) => (
              <div key={field}>
                <dt className="text-micro text-muted-foreground">{label}</dt>
                <dd className="break-all font-mono text-caption">
                  {custom
                    ? String(row[field])
                    : formatReferenceRate(row[field])}
                </dd>
              </div>
            ))}
          </dl>
        </>
      ) : (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.usage.custom_pricing.no_reference)}
        </p>
      )}
    </section>
  );
}

function PriceField({
  label,
  value,
  disabled,
  onChange,
}: {
  label: string;
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  const id = useId();
  return (
    <div className="space-y-1">
      <Label htmlFor={id} className="text-micro text-muted-foreground">
        {label}
      </Label>
      <Input
        id={id}
        type="number"
        inputMode="decimal"
        min="0"
        step="any"
        disabled={disabled}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  );
}
