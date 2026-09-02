"use client";

import { useId, useState } from "react";
import { Trash2 } from "lucide-react";
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
      <DialogContent className="sm:max-w-3xl">
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
  const { t } = useT("runtimes");
  const { role } = useCurrentMember(wsId);
  const canManage =
    snapshot.canManage === true && (role === "owner" || role === "admin");
  const save = useSaveModelPricing(wsId);
  const refresh = useRefreshModelPricing(wsId);
  const [revision, setRevision] = useState(snapshot.revision);
  const createDrafts = (prices: ModelPricingSnapshot) =>
    Object.fromEntries(
      [...new Set([...unmappedModels, ...Object.keys(prices.overrides)])]
        .sort()
        .map((key) => [key, toPriceDraft(prices.overrides[key])]),
    );
  // Keep drafts and their revision together until an explicit reload or save.
  const [drafts, setDrafts] = useState<Record<string, PriceDraft>>(() =>
    createDrafts(snapshot),
  );
  const visibleDrafts = canManage ? drafts : createDrafts(snapshot);
  const [model, setModel] = useState("");
  const [selectedModel, setSelectedModel] = useState("");
  const [legacy] = useState(readLegacyModelPrices);
  const [imported, setImported] = useState<Record<string, ModelPricingRow>>({});
  const [importPreview, setImportPreview] = useState(false);
  const [invalid, setInvalid] = useState(false);
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
  const publicPrices = { ...snapshot, overrides: {} };
  const reference = selectedModel
    ? resolveModelPricing(selectedModel, undefined, publicPrices)
    : undefined;
  const effective = selectedModel
    ? resolveModelPricing(selectedModel, undefined, snapshot)
    : undefined;

  const handleSave = async () => {
    const overrides = parsePriceDrafts(drafts);
    if (!overrides) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    try {
      await save.mutateAsync({ revision, overrides });
      clearImportedModelPrices(
        Object.fromEntries(
          Object.entries(imported).filter(([key]) => key in overrides),
        ),
      );
      onClose();
    } catch {
      // The mutation error is rendered below; drafts remain intact.
    }
  };
  const viewModel = () => {
    const key = model.trim().toLowerCase();
    if (key && key.length <= 512) setSelectedModel(key);
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
      setRevision(latest.revision);
      setDrafts(createDrafts(latest));
      setImported({});
      setImportPreview(false);
      setInvalid(false);
      save.reset();
    } finally {
      setReloading(false);
    }
  };

  return (
    <>
      <div className="max-h-[60vh] space-y-4 overflow-y-auto">
        <p className="text-caption text-muted-foreground">
          {t(($) => $.usage.custom_pricing.api_equivalent)}
        </p>
        <div className="flex flex-wrap items-center justify-between gap-2 text-micro text-muted-foreground">
          <span>
            {t(($) => $.usage.custom_pricing.schedule, {
              timezone: snapshot.timezone,
            })}
            <br />
            {t(($) => $.usage.custom_pricing.last_update, {
              time: snapshot.succeededAt
                ? new Date(snapshot.succeededAt).toLocaleString()
                : t(($) => $.usage.custom_pricing.bundled),
            })}
          </span>
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
        {(snapshot.lastError || refresh.isError) && (
          <p role="status" className="text-caption text-warning">
            {t(($) => $.usage.custom_pricing.refresh_error)}
          </p>
        )}
        {!canManage && (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.usage.custom_pricing.readonly)}
          </p>
        )}
        <div className="space-y-2">
          <div className="flex gap-2">
            <Input
              value={model}
              list={listId}
              maxLength={512}
              aria-label={t(($) => $.usage.custom_pricing.model)}
              placeholder={t(($) => $.usage.custom_pricing.model)}
              onChange={(event) => setModel(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  viewModel();
                }
              }}
            />
            <datalist id={listId}>
              {[
                ...new Set([
                  ...Object.keys(snapshot.rows),
                  ...Object.keys(snapshot.aliases),
                  ...Object.keys(snapshot.overrides),
                ]),
              ]
                .filter((key) => key.includes(model.trim().toLowerCase()))
                .sort()
                .slice(0, 100)
                .map((key) => (
                  <option key={key} value={key} />
                ))}
            </datalist>
            <Button
              variant="outline"
              disabled={!model.trim() || busy}
              onClick={viewModel}
            >
              {t(($) => $.usage.custom_pricing.view)}
            </Button>
          </div>
          {selectedModel && (
            <section
              className="space-y-3 rounded-md bg-muted/40 p-3"
              aria-label={t(($) => $.usage.custom_pricing.reference)}
            >
              <div className="flex flex-wrap items-center justify-between gap-2">
                <code className="break-all font-mono text-caption">
                  {selectedModel}
                </code>
                {canManage && (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={busy || selectedModel in drafts}
                    onClick={() =>
                      setDrafts((previous) => ({
                        ...previous,
                        [selectedModel]: toPriceDraft(effective),
                      }))
                    }
                  >
                    {t(($) => $.usage.custom_pricing.add)}
                  </Button>
                )}
              </div>
              {reference ? (
                <>
                  <p className="text-micro text-muted-foreground">
                    {t(($) => $.usage.custom_pricing.source, {
                      source:
                        reference.source ??
                        t(($) => $.usage.custom_pricing.bundled),
                    })}
                    {reference.sourceUrl &&
                      /^https?:\/\//.test(reference.sourceUrl) && (
                        <>
                          {" "}
                          ·{" "}
                          <a
                            className="underline underline-offset-2"
                            href={reference.sourceUrl}
                            target="_blank"
                            rel="noreferrer"
                          >
                            {t(($) => $.usage.custom_pricing.source_link)}
                          </a>
                        </>
                      )}
                  </p>
                  <dl className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                    {fields.map(([field, label]) => (
                      <div key={field}>
                        <dt className="text-micro text-muted-foreground">
                          {label}
                        </dt>
                        <dd className="break-all font-mono text-caption">
                          {formatReferenceRate(reference[field])}
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
          )}
        </div>
        {Object.entries(visibleDrafts).map(([key, draft]) => (
          <div key={key} className="space-y-2 rounded-md border p-3">
            <div className="flex items-center justify-between gap-2">
              <code className="break-all font-mono text-caption">{key}</code>
              {canManage && (
                <Button
                  variant="ghost"
                  size="icon-xs"
                  disabled={busy}
                  aria-label={t(($) => $.usage.custom_pricing.remove_aria)}
                  onClick={() =>
                    setDrafts((previous) => {
                      const next = { ...previous };
                      delete next[key];
                      return next;
                    })
                  }
                >
                  <Trash2 />
                </Button>
              )}
            </div>
            <p className="text-micro text-muted-foreground">
              {t(($) => $.usage.custom_pricing.override)}
            </p>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              {fields.map(([field, label]) => (
                <PriceField
                  key={field}
                  label={label}
                  value={draft[field]}
                  disabled={!canManage || busy}
                  onChange={(value) =>
                    setDrafts((previous) => ({
                      ...previous,
                      [key]: { ...draft, [field]: value },
                    }))
                  }
                />
              ))}
            </div>
          </div>
        ))}
        {Object.keys(visibleDrafts).length === 0 && (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.usage.custom_pricing.empty)}
          </p>
        )}
        {canManage && Object.keys(legacy).length > 0 && (
          <div className="space-y-2">
            <Button
              variant="outline"
              disabled={busy || importPreview}
              onClick={() => {
                const additions = previewLegacyPrices(
                  legacy,
                  snapshot.overrides,
                  drafts,
                );
                setImported(additions);
                setImportPreview(true);
                setDrafts((previous) => ({
                  ...previous,
                  ...Object.fromEntries(
                    Object.entries(additions).map(([key, row]) => [
                      key,
                      toPriceDraft(row),
                    ]),
                  ),
                }));
              }}
            >
              {t(($) => $.usage.custom_pricing.import_local)}
            </Button>
            {importPreview && (
              <p role="status" className="text-caption text-muted-foreground">
                {Object.keys(imported).length > 0
                  ? t(($) => $.usage.custom_pricing.import_preview, {
                      count: Object.keys(imported).length,
                    })
                  : t(($) => $.usage.custom_pricing.import_empty)}
              </p>
            )}
          </div>
        )}
        <p className="text-micro text-muted-foreground">
          {t(($) => $.usage.custom_pricing.unit_hint)}
        </p>
        {invalid && (
          <p role="alert" className="text-caption text-destructive">
            {t(($) => $.usage.custom_pricing.invalid)}
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
      <DialogFooter>
        <Button variant="outline" disabled={save.isPending} onClick={onClose}>
          {t(($) => $.usage.custom_pricing.cancel)}
        </Button>
        {canManage && (
          <Button disabled={busy || conflict} onClick={() => void handleSave()}>
            {t(($) => $.usage.custom_pricing.save)}
          </Button>
        )}
      </DialogFooter>
    </>
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
