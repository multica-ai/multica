"use client";

import { useNavigation } from "../navigation";
import { DragStrip } from "../platform";
import { ArrowLeft } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";
import { paths } from "@multica/core/paths";
import { useT } from "@multica/i18n/react";
import { CreateWorkspaceForm } from "../workspace/create-workspace-form";

export function CreateWorkspaceModal({ onClose }: { onClose: () => void }) {
  const router = useNavigation();
  const t = useT("onboarding");

  return (
    <Dialog
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
    >
      <DialogContent
        finalFocus={false}
        showCloseButton={false}
        className="inset-0 flex h-full w-full max-w-none sm:max-w-none translate-0 flex-col rounded-none bg-background ring-0 shadow-none"
      >
        {/* DragStrip as flex child — macOS traffic lights stay visible and
            the top 48px is draggable. Back button sits just below the strip
            (top-16 = 64px), clear of both traffic lights (y<=27) and the
            strip (y<=48). `no-drag` is a belt-and-braces guard in case the
            button's layout ever creeps up into the strip zone. */}
        <DragStrip />

        <Button
          variant="ghost"
          size="sm"
          className="absolute top-16 left-12 text-muted-foreground"
          style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
          onClick={onClose}
        >
          <ArrowLeft className="h-4 w-4" />
          {t("back")}
        </Button>

        <div className="flex flex-1 flex-col items-center justify-center px-6 pb-12">
          <div className="flex w-full max-w-md flex-col items-center gap-6">
            <div className="text-center">
              <DialogTitle className="text-2xl font-semibold">
                {t("create_new_workspace")}
              </DialogTitle>
              <DialogDescription className="mt-2">
                {t("workspace_description")}
              </DialogDescription>
            </div>
            <CreateWorkspaceForm
              onSuccess={(newWs) => {
                onClose();
                // Navigate INTO the new workspace. The mutation's own onSuccess
                // (in core/workspace/mutations.ts) runs before this callback and
                // has already seeded the workspace list cache, so the destination
                // [workspaceSlug]/layout will resolve newWs.slug → workspace
                // synchronously without a loading flash.
                router.push(paths.workspace(newWs.slug).issues());
              }}
            />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
