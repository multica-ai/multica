"use client";

import { useMemo } from "react";
import { Download, Mic, Trash2 } from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Progress } from "@multica/ui/components/ui/progress";
import {
  cancelVoice,
  pickVoiceModel,
  toggleVoice,
  useVoiceSettingsStore,
  type VoiceModel,
} from "@multica/core/voice";
import { formatBytes } from "../common/format-bytes";
import { useEditorOverlayOwner } from "../editor/overlay-owner";
import { useLocale, useT } from "../i18n";
import { localVoiceAPI } from "../platform/local-voice";
import { openExternal } from "../platform/open-external";
import { useVoiceModels } from "./engine";
import type { ComposerVoice } from "./use-composer-voice";

const LISTED_LANGUAGES = 5;

/** The first model that speaks the interface language, else the first one. */
function recommendedModel(models: VoiceModel[], locale: string): string | undefined {
  const base = locale.split("-")[0]?.toLowerCase() ?? "";
  return (models.find((model) => model.languages.includes(base)) ?? models[0])?.id;
}

function useLanguageList(languages: string[]): string {
  const { t } = useT("editor");
  const locale = useLocale();
  return useMemo(() => {
    let names: Intl.DisplayNames | null = null;
    try {
      names = new Intl.DisplayNames([locale], { type: "language" });
    } catch {
      names = null;
    }
    const shown = languages.slice(0, LISTED_LANGUAGES).map((code) => names?.of(code) ?? code);
    const more = languages.length - shown.length;
    if (more > 0) shown.push(t(($) => $.voice.models.more_languages, { more }));
    return new Intl.ListFormat(locale, { type: "conjunction" }).format(shown);
  }, [languages, locale, t]);
}

function ModelRow({
  model,
  recommended,
  active,
  manage,
  onDownload,
}: {
  model: VoiceModel;
  recommended: boolean;
  active: boolean;
  manage: boolean;
  onDownload: (id: string) => void;
}) {
  const { t } = useT("editor");
  const update = useVoiceSettingsStore((state) => state.update);
  const languages = useLanguageList(model.languages);
  const api = localVoiceAPI();
  const percent = model.sizeBytes > 0 ? Math.floor((model.receivedBytes / model.sizeBytes) * 100) : 0;
  const download = () => {
    onDownload(model.id);
    void api?.downloadModel(model.id);
  };
  return (
    <div className="flex items-center gap-4 px-4 py-3.5">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2 text-body font-medium">
          {model.name}
          {active && <Badge variant="secondary">{t(($) => $.voice.models.in_use)}</Badge>}
          {recommended && !active && <Badge variant="outline">{t(($) => $.voice.models.recommended)}</Badge>}
          {model.highlight && (
            <Badge variant="outline">
              {model.highlight === "accurate"
                ? t(($) => $.voice.models.most_accurate)
                : t(($) => $.voice.models.fastest)}
            </Badge>
          )}
        </div>
        <div className="mt-0.5 text-caption leading-5 text-muted-foreground">
          {languages} · <span className="whitespace-nowrap">{formatBytes(model.sizeBytes)}</span>
        </div>
        <div className="text-caption leading-5 text-muted-foreground">
          {t(($) => $.voice.models.by, { author: model.author })} ·{" "}
          <button
            type="button"
            className="underline underline-offset-3 hover:text-foreground"
            onClick={() => openExternal(model.licenseUrl)}
          >
            {t(($) => $.voice.models.license)}
          </button>
        </div>
        {model.status === "downloading" && (
          <Progress value={percent} className="mt-2" aria-label={t(($) => $.voice.models.downloading, { percent })} />
        )}
        {model.status === "absent" && model.error && (
          <div role="alert" className="mt-1 text-caption leading-5 text-destructive">
            {t(($) => $.voice.models.failed)}
          </div>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {model.status === "absent" && (
          <Button size="xs" variant="outline" onClick={download}>
            <Download />
            {model.error ? t(($) => $.voice.models.retry) : t(($) => $.voice.models.download)}
          </Button>
        )}
        {model.status === "downloading" && (
          <>
            <span className="text-caption tabular-nums text-muted-foreground">{percent}%</span>
            <Button size="xs" variant="ghost" onClick={() => void api?.cancelDownload(model.id)}>
              {t(($) => $.voice.models.cancel)}
            </Button>
          </>
        )}
        {model.status === "ready" && manage && (
          <>
            {!active && (
              <Button size="xs" variant="outline" onClick={() => update({ modelId: model.id })}>
                {t(($) => $.voice.models.use)}
              </Button>
            )}
            <IconButton
              label={t(($) => $.voice.models.delete, { name: model.name })}
              onClick={() => void api?.deleteModel(model.id)}
            >
              <Trash2 />
            </IconButton>
          </>
        )}
      </div>
    </div>
  );
}

/**
 * Desktop's speech models as rows, for a bordered list. `manage` adds
 * choosing between installed models and deleting them.
 */
export function VoiceModelRows({
  manage = false,
  onDownload = () => {},
}: {
  manage?: boolean;
  onDownload?: (id: string) => void;
}) {
  const snapshot = useVoiceModels();
  const preferred = useVoiceSettingsStore((state) => state.modelId);
  const locale = useLocale();
  if (!snapshot) return null;
  const active = pickVoiceModel(snapshot, preferred);
  const recommended = recommendedModel(snapshot.models, locale);
  return (
    <>
      {snapshot.models.map((model) => (
        <ModelRow
          key={model.id}
          model={model}
          active={model.id === active?.id}
          recommended={model.id === recommended}
          manage={manage}
          onDownload={onDownload}
        />
      ))}
    </>
  );
}

/** Shown when the mic is pressed before any speech model is installed. */
export function VoiceSetupDialog({ voice }: { voice: ComposerVoice }) {
  const { t } = useT("editor");
  const snapshot = useVoiceModels();
  const preferred = useVoiceSettingsStore((state) => state.modelId);
  const update = useVoiceSettingsStore((state) => state.update);
  const ready = pickVoiceModel(snapshot, preferred) !== null;
  // Opened from a popover composer (an annotation), it must not dismiss it.
  const overlayOwner = useEditorOverlayOwner();
  return (
    <Dialog
      open={voice.showSetup}
      onOpenChange={(open) => {
        if (!open) cancelVoice();
      }}
    >
      <DialogContent className="sm:max-w-lg" data-editor-overlay-owner={overlayOwner}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.voice.setup.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.voice.setup.description)}</DialogDescription>
        </DialogHeader>
        <div className="divide-y divide-surface-border rounded-lg border border-surface-border">
          <VoiceModelRows onDownload={(id) => update({ modelId: id })} />
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={cancelVoice}>
            {t(($) => $.voice.setup.later)}
          </Button>
          <Button disabled={!ready} onClick={() => toggleVoice(voice.id)}>
            <Mic />
            {t(($) => $.voice.setup.start)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
