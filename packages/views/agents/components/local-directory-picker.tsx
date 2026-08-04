import { useState } from "react";
import { AlertTriangle, Check, FolderOpen, Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";
import {
  isDesktopShell,
  pickDirectory,
  validateLocalDirectory,
} from "../../platform/local-directory";

/**
 * Local-directory picker for directory-based agents.
 *
 * Desktop (Electron) shell: a "Browse" button opens the native directory
 * chooser via `desktopAPI.pickDirectory`. Web has no filesystem access, so it
 * falls back to a manual absolute-path input — the daemon validates the path
 * at claim time. Both surfaces offer a Validate button when the API is
 * available (desktop only).
 */
export function LocalDirectoryPicker({
  value,
  onChange,
}: {
  value: string;
  onChange: (path: string) => void;
}) {
  const { t } = useT("agents");
  const desktop = isDesktopShell();
  const [validating, setValidating] = useState(false);
  const [valid, setValid] = useState<boolean | null>(null);
  const [error, setError] = useState<string | null>(null);

  const handlePick = async () => {
    const res = await pickDirectory();
    if (res.ok && res.path) {
      onChange(res.path);
      setValid(null);
      setError(null);
    }
  };

  const handleValidate = async () => {
    const path = value.trim();
    if (!path) {
      setValid(false);
      setError(t(($) => $.creation_studio.directory_required));
      return;
    }
    setValidating(true);
    const res = await validateLocalDirectory(path);
    setValidating(false);
    if (res.ok) {
      setValid(true);
      setError(null);
    } else {
      setValid(false);
      setError(
        res.reason === "unsupported"
          ? t(($) => $.creation_studio.directory_unsupported)
          : t(($) => $.creation_studio.directory_invalid),
      );
    }
  };

  const updatePath = (path: string) => {
    onChange(path);
    setValid(null);
    setError(null);
  };

  return (
    <div className="flex flex-col gap-3 px-4 py-4">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          value={value}
          onChange={(e) => updatePath(e.target.value)}
          placeholder={t(($) => $.creation_studio.directory_placeholder)}
          aria-label={t(($) => $.creation_studio.directory_label)}
          className="min-w-64 flex-1 font-mono text-label"
        />
        {desktop && (
          <Button type="button" variant="outline" onClick={handlePick}>
            <FolderOpen className="size-4" />
            {t(($) => $.creation_studio.directory_browse)}
          </Button>
        )}
        <Button
          type="button"
          variant="secondary"
          onClick={handleValidate}
          disabled={validating}
        >
          {validating ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Check className="size-4" />
          )}
          {t(($) => $.creation_studio.directory_validate)}
        </Button>
      </div>
      {!desktop && (
        <p className="text-caption leading-5 text-muted-foreground">
          {t(($) => $.creation_studio.directory_web_hint)}
        </p>
      )}
      {error && (
        <p className="flex items-center gap-1.5 text-caption text-destructive">
          <AlertTriangle className="size-3.5" />
          {error}
        </p>
      )}
      {valid === true && (
        <p className="flex items-center gap-1.5 text-caption text-emerald-600">
          <Check className="size-3.5" />
          {t(($) => $.creation_studio.directory_valid)}
        </p>
      )}
    </div>
  );
}
