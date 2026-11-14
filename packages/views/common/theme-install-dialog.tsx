"use client";

import { useState } from "react";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  useAppearanceStore,
  useInstalledThemesStore,
  type Theme,
  type ThemePackageResult,
} from "@multica/core/appearance";
import { useT } from "../i18n";

export interface ThemeInstallDialog {
  /** Shows a theme that was read for confirmation, or the problems that stop it installing. */
  offer: (result: ThemePackageResult) => void;
  /** The confirm and error dialogs; render it once. */
  element: React.ReactNode;
}

/**
 * Confirms installing a theme read from a file — Import theme, or a .zip an
 * agent attached — showing what it is and that it can restyle every page,
 * and switches to it on confirmation. Problems are listed as the checks
 * report them, which is what someone writing a theme needs.
 */
export function useThemeInstallDialog(): ThemeInstallDialog {
  const { t } = useT("settings");
  const [pending, setPending] = useState<Theme | null>(null);
  const [errors, setErrors] = useState<string[] | null>(null);
  const installed = useInstalledThemesStore((s) => s.themes);
  const install = useInstalledThemesStore((s) => s.install);
  const uninstall = useInstalledThemesStore((s) => s.uninstall);
  const set = useAppearanceStore((s) => s.set);
  const previous = pending ? installed.find((theme) => theme.id === pending.id) : undefined;

  const offer = (result: ThemePackageResult) => {
    if (result.ok) setPending(result.theme);
    else setErrors(result.errors);
  };

  const confirm = () => {
    if (!pending) return;
    const replaced = previous;
    try {
      install(pending);
      set({ theme: pending.id });
    } catch {
      if (replaced) install(replaced);
      else uninstall(pending.id);
      setErrors([t(($) => $.appearance.import.storage_full)]);
    }
    setPending(null);
  };

  const element = (
    <>
      <AlertDialog open={pending !== null} onOpenChange={(open) => !open && setPending(null)}>
        <AlertDialogContent>
          {pending ? <ConfirmContent theme={pending} previous={previous} onConfirm={confirm} /> : null}
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={errors !== null} onOpenChange={(open) => !open && setErrors(null)}>
        <AlertDialogContent>{errors ? <ErrorsContent errors={errors} /> : null}</AlertDialogContent>
      </AlertDialog>
    </>
  );
  return { offer, element };
}

function ConfirmContent({ theme, previous, onConfirm }: { theme: Theme; previous?: Theme; onConfirm: () => void }) {
  const { t } = useT("settings");
  const assetCount = Object.keys(theme.assets ?? {}).length;
  const optionCount = theme.options?.filter((option) => option.type !== "heading").length ?? 0;
  return (
    <>
      <AlertDialogHeader>
        <AlertDialogTitle className="flex items-center gap-2">
          <span className="min-w-0 truncate">{t(($) => $.appearance.import.confirm_title, { name: theme.name })}</span>
          <Badge variant="secondary">{t(($) => $.appearance.import.beta)}</Badge>
        </AlertDialogTitle>
        <AlertDialogDescription>{t(($) => $.appearance.import.warning)}</AlertDialogDescription>
      </AlertDialogHeader>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 rounded-md bg-muted px-3 py-2.5 text-caption">
        {theme.author ? (
          <>
            <dt className="text-muted-foreground">{t(($) => $.appearance.import.author)}</dt>
            <dd className="min-w-0 truncate">{theme.author}</dd>
          </>
        ) : null}
        {theme.version ? (
          <>
            <dt className="text-muted-foreground">{t(($) => $.appearance.import.version)}</dt>
            <dd>
              {previous?.version
                ? t(($) => $.appearance.import.replaces, { from: previous.version, to: theme.version })
                : theme.version}
            </dd>
          </>
        ) : null}
        <dt className="text-muted-foreground">{t(($) => $.appearance.import.includes)}</dt>
        <dd>
          {[
            "theme.css",
            assetCount > 0 ? t(($) => $.appearance.import.has_assets, { count: assetCount }) : null,
            optionCount > 0 ? t(($) => $.appearance.import.has_options, { count: optionCount }) : null,
          ]
            .filter(Boolean)
            .join(" · ")}
        </dd>
      </dl>
      <p className="text-caption text-muted-foreground">{t(($) => $.appearance.import.beta_note)}</p>
      <AlertDialogFooter>
        <AlertDialogCancel>{t(($) => $.appearance.import.cancel)}</AlertDialogCancel>
        <AlertDialogAction onClick={onConfirm}>{t(($) => $.appearance.import.confirm)}</AlertDialogAction>
      </AlertDialogFooter>
    </>
  );
}

function ErrorsContent({ errors }: { errors: readonly string[] }) {
  const { t } = useT("settings");
  return (
    <>
      <AlertDialogHeader>
        <AlertDialogTitle>{t(($) => $.appearance.import.error_title)}</AlertDialogTitle>
        <AlertDialogDescription>{t(($) => $.appearance.import.error_description)}</AlertDialogDescription>
      </AlertDialogHeader>
      <ul className="max-h-60 list-disc space-y-1 overflow-y-auto rounded-md bg-muted py-2.5 pr-3 pl-7 font-mono text-caption">
        {errors.map((error, i) => (
          <li key={i}>{error}</li>
        ))}
      </ul>
      <AlertDialogFooter>
        <AlertDialogCancel>{t(($) => $.appearance.import.close)}</AlertDialogCancel>
      </AlertDialogFooter>
    </>
  );
}
