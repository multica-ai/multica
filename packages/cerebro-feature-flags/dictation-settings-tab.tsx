// FIR-1797 — inline settings for the Dictation feature, shown under its toggle
// in the Cerebro features tab (registered in FLAG_SETTINGS). Lets the user keep
// a personal glossary (post-transcription correction) and cleanup on/off.
"use client";

import { useEffect, useState } from "react";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { toast } from "sonner";
import { useDictationSettings, useSetDictationSettings } from "./dictation-settings";

export function DictationSettings() {
  const settings = useDictationSettings();
  const setSettings = useSetDictationSettings();

  // Local draft so typing in the glossary stays responsive; persist on blur.
  const [glossaryDraft, setGlossaryDraft] = useState(settings.glossary);
  useEffect(() => {
    setGlossaryDraft(settings.glossary);
  }, [settings.glossary]);

  const persistGlossary = () => {
    if (glossaryDraft === settings.glossary) return;
    void setSettings({ glossary: glossaryDraft }).catch((err) => {
      toast.error(err instanceof Error ? err.message : "Failed to save glossary");
      setGlossaryDraft(settings.glossary);
    });
  };

  const toggleCleanup = (checked: boolean) => {
    void setSettings({ cleanup: checked }).catch((err) => {
      toast.error(err instanceof Error ? err.message : "Failed to update cleanup");
    });
  };

  return (
    <div className="space-y-4 pt-1">
      <div className="space-y-1.5">
        <Label className="text-sm font-medium">Extra glossary terms</Label>
        <p className="text-xs text-muted-foreground">
          Workspace and business-object names are checked automatically after
          transcription. Add anything else here — brand and product names or
          jargon — one per line.
        </p>
        <Textarea
          value={glossaryDraft}
          onChange={(e) => setGlossaryDraft(e.target.value)}
          onBlur={persistGlossary}
          rows={4}
          placeholder={"Helsebixen\nmade4men\n…"}
          className="text-sm"
        />
      </div>
      <div className="flex items-start justify-between gap-3 border-t border-border/60 pt-3">
        <div className="min-w-0">
          <Label className="text-sm font-medium">Clean up text</Label>
          <p className="text-xs text-muted-foreground">
            Fix punctuation, capitalisation and paragraph breaks after
            transcribing. Keeps your words; only tidies the formatting.
          </p>
        </div>
        <Switch checked={settings.cleanup} onCheckedChange={toggleCleanup} />
      </div>
    </div>
  );
}
