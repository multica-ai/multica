"use client";

import { useState } from "react";
import { useWorkspacePaths } from "@multica/core/paths";
import { useCreateEpic } from "@multica/core/epics";
import { DEFAULT_EPIC_COLORS } from "@multica/core/epics/config";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";

export function CreateEpicModal({ onClose }: { onClose: () => void }) {
  const { t } = useT("epics");
  const router = useNavigation();
  const wsPaths = useWorkspacePaths();
  const createEpic = useCreateEpic();

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [color, setColor] = useState(DEFAULT_EPIC_COLORS[0]);

  const canSubmit = !!title.trim() && !createEpic.isPending;

  const handleSubmit = () => {
    if (!canSubmit) return;
    createEpic.mutate(
      {
        title: title.trim(),
        description: description.trim() || undefined,
        color,
      },
      {
        onSuccess: (epic) => {
          onClose();
          toast.success("Epic created");
          router.push(wsPaths.epicDetail(epic.id));
        },
        onError: (err) => {
          toast.error(err instanceof Error ? err.message : "Failed to create epic");
        },
      },
    );
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="p-0 gap-0 flex flex-col overflow-hidden !top-1/2 !left-1/2 !-translate-x-1/2 !-translate-y-1/2 !w-full !max-w-lg">
        <DialogHeader className="border-b px-5 py-3 space-y-0">
          <DialogTitle className="text-base font-semibold">
            {t(($) => $.create.title)}
          </DialogTitle>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto p-5 space-y-4">
          <div>
            <Label className="text-xs text-muted-foreground">{t(($) => $.create.label_title)}</Label>
            <Input
              autoFocus
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t(($) => $.create.title_placeholder)}
              className="mt-1"
              onKeyDown={(e) => {
                if (e.key === "Enter") handleSubmit();
              }}
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">{t(($) => $.create.label_description)}</Label>
            <Input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t(($) => $.create.description_placeholder)}
              className="mt-1"
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.create.color)}
            </Label>
            <div className="flex items-center gap-2 mt-2">
              {DEFAULT_EPIC_COLORS.map((c) => (
                <button
                  key={c}
                  type="button"
                  onClick={() => setColor(c)}
                  className={cn(
                    "size-6 rounded-full transition-all",
                    color === c
                      ? "ring-2 ring-offset-2 ring-foreground scale-110"
                      : "hover:scale-110",
                  )}
                  style={{ backgroundColor: c }}
                />
              ))}
            </div>
          </div>
        </div>

        <div className="flex items-center justify-end gap-2 border-t bg-background px-5 py-3">
          <Button variant="ghost" onClick={onClose}>
            {t(($) => $.create.cancel)}
          </Button>
          <Button onClick={handleSubmit} disabled={!canSubmit}>
            {createEpic.isPending ? "..." : t(($) => $.create.submit)}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
