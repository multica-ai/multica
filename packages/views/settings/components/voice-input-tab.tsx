"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { useShortcut } from "@multica/core/shortcuts";
import {
  MAX_VOCABULARY_WORDS,
  pickVoiceModel,
  useVoiceSettingsStore,
  VOICE_LANGUAGES,
  type VoiceActivation,
  type VoiceLanguage,
} from "@multica/core/voice";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { ShortcutKeycaps } from "../../common/shortcut-keycaps";
import { AppLink, useOptionalNavigation } from "../../navigation";
import { useLocale, useT } from "../../i18n";
import { useVoiceModels, VoiceModelRows, voiceInputSupported } from "../../voice";
import { settingsHref } from "./settings-navigation";
import { VoiceHistorySection } from "./voice-history";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

/**
 * Speech models and how dictation behaves on this computer. Desktop only:
 * transcription runs on the device, so the tab needs Desktop's engine.
 */
export function VoiceInputTab() {
  const { t } = useT("settings");
  const engineAvailable = useVoiceModels()?.engineAvailable;
  return (
    <SettingsTab
      title={t(($) => $.page.tabs.voice_input)}
      description={t(($) => $.voice_input.description)}
      scope="device"
    >
      <SettingsSection
        title={t(($) => $.voice_input.model_title)}
        description={
          engineAvailable === false
            ? t(($) => $.voice_input.engine_unavailable)
            : t(($) => $.voice_input.model_hint)
        }
        anchor="voice-model"
      >
        {engineAvailable && (
          <SettingsCard>
            <VoiceModelRows manage />
          </SettingsCard>
        )}
      </SettingsSection>
      <SettingsSection title={t(($) => $.voice_input.input_title)} anchor="voice-input">
        <SettingsCard>
          <ShortcutRow />
          <ActivationRow />
        </SettingsCard>
      </SettingsSection>
      <SettingsSection
        title={t(($) => $.voice_input.recognition_title)}
        anchor="voice-recognition"
      >
        <SettingsCard>
          <LanguageRow />
          <FillersRow />
          <VocabularyRow />
        </SettingsCard>
      </SettingsSection>
      <SettingsSection
        title={t(($) => $.voice_input.microphone_title)}
        anchor="voice-microphone"
      >
        <SettingsCard>
          <MicrophoneRows />
        </SettingsCard>
      </SettingsSection>
      <VoiceHistorySection />
    </SettingsTab>
  );
}

function ShortcutRow() {
  const { t } = useT("settings");
  const shortcut = useShortcut("voiceInput");
  const navigation = useOptionalNavigation();
  const href = navigation
    ? settingsHref(navigation.pathname, navigation.searchParams, "shortcuts")
    : "?tab=shortcuts";
  return (
    <SettingsRow
      anchor="voice-shortcut"
      label={t(($) => $.voice_input.shortcut.title)}
      description={t(($) => $.voice_input.shortcut.hint)}
    >
      <div className="flex items-center gap-3">
        {shortcut ? (
          <ShortcutKeycaps shortcut={shortcut} size="md" />
        ) : (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.voice_input.shortcut.unassigned)}
          </span>
        )}
        <Button size="xs" variant="outline" render={<AppLink href={href} />} nativeButton={false}>
          {t(($) => $.voice_input.shortcut.change)}
        </Button>
      </div>
    </SettingsRow>
  );
}

function ActivationRow() {
  const { t } = useT("settings");
  const activation = useVoiceSettingsStore((state) => state.activation);
  const update = useVoiceSettingsStore((state) => state.update);
  const label = t(($) => $.voice_input.activation.title);
  return (
    <SettingsRow
      anchor="voice-activation"
      label={label}
      description={t(($) => $.voice_input.activation.hints[activation])}
    >
      <div role="group" aria-label={label}>
        <SegmentedToggle<VoiceActivation>
          value={activation}
          onChange={(next) => update({ activation: next })}
          buttonClassName="px-3 py-1 text-label"
          options={[
            ["auto", t(($) => $.voice_input.activation.auto)],
            ["toggle", t(($) => $.voice_input.activation.toggle)],
            ["hold", t(($) => $.voice_input.activation.hold)],
          ]}
        />
      </div>
    </SettingsRow>
  );
}

function LanguageRow() {
  const { t } = useT("settings");
  const locale = useLocale();
  const language = useVoiceSettingsStore((state) => state.language);
  const modelId = useVoiceSettingsStore((state) => state.modelId);
  const update = useVoiceSettingsStore((state) => state.update);
  const model = pickVoiceModel(useVoiceModels(), modelId);
  const label = t(($) => $.voice_input.language.title);
  const options = useMemo(() => {
    let names: Intl.DisplayNames | null = null;
    try {
      names = new Intl.DisplayNames([locale], { type: "language" });
    } catch {
      names = null;
    }
    return VOICE_LANGUAGES.map((value) => ({
      value,
      label:
        value === "auto"
          ? t(($) => $.voice_input.language.auto)
          : (names?.of(value) ?? value),
    }));
  }, [locale, t]);
  // Models without a language hint always detect the language themselves.
  if (model && !model.languageHint) return null;
  return (
    <SettingsRow
      anchor="voice-language"
      label={label}
      description={t(($) => $.voice_input.language.hint)}
      size="select"
    >
      <Select
        items={options}
        value={language}
        onValueChange={(next) => {
          if (next) update({ language: next as VoiceLanguage });
        }}
      >
        <SelectTrigger size="sm" className="w-full" aria-label={label}>
          <SelectValue>{options.find((option) => option.value === language)?.label}</SelectValue>
        </SelectTrigger>
        <SelectContent align="end">
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </SettingsRow>
  );
}

function FillersRow() {
  const { t } = useT("settings");
  const removeFillers = useVoiceSettingsStore((state) => state.removeFillers);
  const update = useVoiceSettingsStore((state) => state.update);
  const label = t(($) => $.voice_input.fillers.title);
  return (
    <SettingsRow
      anchor="voice-fillers"
      label={label}
      description={t(($) => $.voice_input.fillers.hint)}
    >
      <Switch
        checked={removeFillers}
        onCheckedChange={(checked) => update({ removeFillers: checked })}
        aria-label={label}
      />
    </SettingsRow>
  );
}

function VocabularyRow() {
  const { t } = useT("settings");
  const vocabulary = useVoiceSettingsStore((state) => state.vocabulary);
  const addWord = useVoiceSettingsStore((state) => state.addWord);
  const removeWord = useVoiceSettingsStore((state) => state.removeWord);
  const [draft, setDraft] = useState("");
  const full = vocabulary.length >= MAX_VOCABULARY_WORDS;
  const label = t(($) => $.voice_input.vocabulary.title);
  const add = () => {
    addWord(draft);
    setDraft("");
  };
  return (
    <SettingsRow
      anchor="voice-vocabulary"
      label={label}
      description={t(($) => $.voice_input.vocabulary.hint, { max: MAX_VOCABULARY_WORDS })}
      size="text"
      align="start"
    >
      <div className="flex min-h-8 flex-wrap items-center gap-1 rounded-md border border-input px-1.5 py-1 focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50">
        {vocabulary.map((word) => (
          <span
            key={word}
            className="inline-flex h-6 items-center gap-1 rounded-sm bg-muted pl-2 pr-1 text-caption"
          >
            {word}
            <button
              type="button"
              aria-label={t(($) => $.voice_input.vocabulary.remove, { word })}
              onClick={() => removeWord(word)}
              className="flex size-4 items-center justify-center rounded-xs text-muted-foreground hover:bg-surface-hover hover:text-foreground"
            >
              <X className="size-3" aria-hidden />
            </button>
          </span>
        ))}
        {!full && (
          <input
            value={draft}
            aria-label={label}
            onChange={(event) => setDraft(event.target.value)}
            onBlur={() => {
              if (draft.trim()) add();
            }}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.nativeEvent.isComposing) {
                event.preventDefault();
                add();
              } else if (event.key === "Backspace" && !draft && vocabulary.length > 0) {
                removeWord(vocabulary[vocabulary.length - 1]!);
              }
            }}
            placeholder={t(($) => $.voice_input.vocabulary.placeholder)}
            className="h-6 min-w-24 flex-1 bg-transparent px-1 text-caption outline-none placeholder:text-muted-foreground"
          />
        )}
      </div>
    </SettingsRow>
  );
}

// Chrome lists pseudo-devices that duplicate a real one.
const PSEUDO_DEVICE_IDS = new Set(["default", "communications"]);
const DEFAULT_DEVICE_VALUE = "__default__";

function useAudioInputs() {
  const [inputs, setInputs] = useState<MediaDeviceInfo[]>([]);
  const refresh = useCallback(() => {
    if (!navigator.mediaDevices?.enumerateDevices) return;
    void navigator.mediaDevices.enumerateDevices().then(
      (devices) =>
        setInputs(
          devices.filter(
            (device) => device.kind === "audioinput" && !PSEUDO_DEVICE_IDS.has(device.deviceId),
          ),
        ),
      () => setInputs([]),
    );
  }, []);
  useEffect(() => {
    if (!navigator.mediaDevices) return;
    refresh();
    navigator.mediaDevices.addEventListener("devicechange", refresh);
    return () => navigator.mediaDevices.removeEventListener("devicechange", refresh);
  }, [refresh]);
  return { inputs, refresh };
}

type MicTest = { state: "off" } | { state: "on"; level: number } | { state: "blocked" };

/** Live input level from the chosen microphone; nothing is recorded. */
function useMicTest(deviceId: string | null, onGranted: () => void) {
  const [test, setTest] = useState<MicTest>({ state: "off" });
  const running = test.state === "on";
  useEffect(() => {
    if (!running) return;
    let cancelled = false;
    let stream: MediaStream | null = null;
    let context: AudioContext | null = null;
    let timer: number | null = null;
    const audio: MediaTrackConstraints = deviceId ? { deviceId: { exact: deviceId } } : {};
    navigator.mediaDevices.getUserMedia({ audio }).then(
      (granted) => {
        if (cancelled) {
          granted.getTracks().forEach((track) => track.stop());
          return;
        }
        stream = granted;
        onGranted();
        context = new AudioContext();
        const analyser = context.createAnalyser();
        analyser.fftSize = 512;
        context.createMediaStreamSource(granted).connect(analyser);
        const samples = new Uint8Array(analyser.fftSize);
        timer = window.setInterval(() => {
          analyser.getByteTimeDomainData(samples);
          let sum = 0;
          for (const sample of samples) {
            const v = (sample - 128) / 128;
            sum += v * v;
          }
          const level = Math.min(1, Math.sqrt(sum / samples.length) * 5);
          setTest((prev) =>
            prev.state === "on" ? { state: "on", level: prev.level * 0.4 + level * 0.6 } : prev,
          );
        }, 50);
      },
      () => {
        if (!cancelled) setTest({ state: "blocked" });
      },
    );
    return () => {
      cancelled = true;
      if (timer !== null) window.clearInterval(timer);
      stream?.getTracks().forEach((track) => track.stop());
      void context?.close().catch(() => {});
    };
  }, [running, deviceId, onGranted]);
  return {
    test,
    start: () => setTest({ state: "on", level: 0 }),
    stop: () => setTest({ state: "off" }),
  };
}

const METER_SEGMENTS = 16;

function MicrophoneRows() {
  const { t } = useT("settings");
  const deviceId = useVoiceSettingsStore((state) => state.deviceId);
  const update = useVoiceSettingsStore((state) => state.update);
  const { inputs, refresh } = useAudioInputs();
  const { test, start, stop } = useMicTest(deviceId, refresh);
  const supported = voiceInputSupported();
  const deviceLabel = t(($) => $.voice_input.device.title);
  const options = [
    { value: DEFAULT_DEVICE_VALUE, label: t(($) => $.voice_input.device.default) },
    ...inputs.map((input, index) => ({
      value: input.deviceId,
      label: input.label || t(($) => $.voice_input.device.unnamed, { index: index + 1 }),
    })),
  ];
  // An unplugged device reads as the default, which is what recording falls
  // back to; the saved choice comes back when the device does.
  const value =
    deviceId && options.some((option) => option.value === deviceId) ? deviceId : DEFAULT_DEVICE_VALUE;
  const level = test.state === "on" ? test.level : 0;
  const unlabeled = inputs.length > 0 && inputs.every((input) => !input.label);
  return (
    <>
      <SettingsRow
        anchor="voice-device"
        label={deviceLabel}
        description={unlabeled ? t(($) => $.voice_input.device.permission_hint) : undefined}
        size="select-wide"
      >
        <Select
          items={options}
          value={value}
          onValueChange={(next) => {
            if (next) update({ deviceId: next === DEFAULT_DEVICE_VALUE ? null : next });
          }}
        >
          <SelectTrigger size="sm" className="w-full" aria-label={deviceLabel}>
            <SelectValue>{options.find((option) => option.value === value)?.label}</SelectValue>
          </SelectTrigger>
          <SelectContent align="end">
            {options.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </SettingsRow>
      <SettingsRow
        anchor="voice-test"
        label={t(($) => $.voice_input.test.title)}
        description={
          test.state === "blocked"
            ? t(($) => $.voice_input.test.blocked)
            : t(($) => $.voice_input.test.hint)
        }
        size="select-wide"
      >
        <div className="flex items-center gap-3">
          <div className="flex h-3 flex-1 items-center gap-[3px]" aria-hidden>
            {Array.from({ length: METER_SEGMENTS }, (_, index) => (
              <span
                key={index}
                className={cn(
                  "h-full flex-1 rounded-xs transition-colors duration-75",
                  index / METER_SEGMENTS < level
                    ? index > METER_SEGMENTS * 0.8
                      ? "bg-warning"
                      : "bg-success"
                    : "bg-muted",
                )}
              />
            ))}
          </div>
          <Button
            size="xs"
            variant="outline"
            disabled={!supported}
            onClick={test.state === "on" ? stop : start}
          >
            {test.state === "on" ? t(($) => $.voice_input.test.stop) : t(($) => $.voice_input.test.start)}
          </Button>
        </div>
      </SettingsRow>
    </>
  );
}
