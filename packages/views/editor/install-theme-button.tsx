"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Loader2, Palette } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import { THEME_PACKAGE_MAX_BYTES, readThemePackage } from "@multica/core/appearance";
import { useThemeInstallDialog } from "../common/theme-install-dialog";
import { useT } from "../i18n";

/**
 * Whether an attachment could be a theme: a .zip no larger than a theme may
 * be, which Import theme would refuse anyway. Its contents are read on click.
 */
export function canBeThemeZip(filename: string, sizeBytes: number | undefined): boolean {
  return /\.zip$/i.test(filename) && (sizeBytes === undefined || sizeBytes <= THEME_PACKAGE_MAX_BYTES);
}

/**
 * Installs a .zip attachment as a theme, through the same checks and confirm
 * dialog as Settings → Appearance → Import theme. This is how a theme an
 * agent built reaches someone on web or on another machine.
 */
export function InstallThemeButton({ attachmentId, filename }: { attachmentId: string; filename: string }) {
  const { t } = useT("editor");
  const dialog = useThemeInstallDialog();
  const [reading, setReading] = useState(false);

  const install = async () => {
    setReading(true);
    try {
      const blob = await api.getAttachmentBlob(attachmentId);
      dialog.offer(await readThemePackage([new File([blob], filename)]));
    } catch {
      toast.error(t(($) => $.attachment.install_theme_failed));
    } finally {
      setReading(false);
    }
  };

  return (
    <>
      <Button
        variant="outline"
        size="xs"
        className="relative shrink-0"
        disabled={reading}
        aria-busy={reading}
        onMouseDown={(event) => event.stopPropagation()}
        onClick={(event) => {
          event.stopPropagation();
          void install();
        }}
      >
        {reading ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Palette data-icon="inline-start" />}
        {t(($) => $.attachment.install_theme)}
      </Button>
      {dialog.element}
    </>
  );
}
