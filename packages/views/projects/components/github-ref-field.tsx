"use client";

import { useId } from "react";
import {
  looksLikeCommitSha,
  validateGitRef,
  type GitRefInvalidReason,
} from "@multica/core/github";
import { useT } from "../../i18n/use-t";

/**
 * Free-text entry for the branch a project's tasks work on.
 *
 * Asks for a BRANCH specifically, though the stored field accepts any ref the
 * daemon can resolve. The reason is delivery: a pinned starting point is also
 * where tasks open their pull requests, and a tag or a commit has nothing to
 * merge back into. Tags and commits remain reachable per task through
 * `multica repo checkout --ref`, which is where a one-off revision belongs.
 *
 * That promise cannot be enforced here — `v1.2.3` is a legal branch name and
 * `main` is a legal tag, so telling them apart means asking the remote, which
 * the product deliberately does not do. The one shape that is unambiguous is a
 * full-length object id, and only that is declined.
 *
 * Free text rather than a dropdown for the same reason: nothing in the product
 * can list a repository's branches today — the server never touches the
 * repository, and the daemon holding the bare caches may be offline or lack
 * access to a private repo.
 *
 * Shared by the create-project modal and the resource panel so the same
 * decision reads the same way wherever it is made.
 */
export function GithubRefField({
  value,
  onChange,
  onSubmit,
  autoFocus,
  id: providedId,
}: {
  value: string;
  onChange: (next: string) => void;
  /** Enter in the field — lets a dialog save without reaching for the mouse. */
  onSubmit?: () => void;
  autoFocus?: boolean;
  /** Optional stable id; one is generated when omitted. */
  id?: string;
}) {
  const { t } = useT("projects");
  // Generated rather than required, because a caller that renders one field
  // PER REPO cannot supply a static id and would otherwise pass none — which
  // silently detaches the label from its input and leaves the field unnamed
  // for a screen reader and untargetable by getByLabelText.
  const generatedId = useId();
  const id = providedId ?? generatedId;
  const error = refErrorMessage(value, t);

  return (
    <div className="space-y-1">
      <label htmlFor={id} className="text-caption font-medium">
        {t(($) => $.resources.ref_label)}
      </label>
      <input
        id={id}
        type="text"
        value={value}
        autoFocus={autoFocus}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && onSubmit) {
            e.preventDefault();
            onSubmit();
          }
        }}
        spellCheck={false}
        autoCapitalize="none"
        autoCorrect="off"
        placeholder={t(($) => $.resources.ref_placeholder)}
        aria-invalid={error !== null}
        aria-describedby={`${id}-hint`}
        className="h-8 w-full rounded-md border bg-transparent px-2 font-mono text-caption outline-none placeholder:font-sans placeholder:text-muted-foreground focus-visible:ring-1 focus-visible:ring-ring aria-invalid:border-destructive"
      />
      <p
        id={`${id}-hint`}
        className={`text-micro ${error ? "text-destructive" : "text-muted-foreground"}`}
      >
        {error ?? t(($) => $.resources.ref_hint)}
      </p>
    </div>
  );
}

/** True when the field holds something this form will not submit. */
export function githubRefHasError(value: string): boolean {
  return validateGitRef(value).ok === false || looksLikeCommitSha(value);
}

/** The message to show, or null when the value is acceptable. */
function refErrorMessage(
  value: string,
  t: ReturnType<typeof useT<"projects">>["t"],
): string | null {
  // Checked before the grammar: a SHA is a perfectly valid ref to store, so
  // validateGitRef passes it. What makes it wrong HERE is that this field
  // names a branch to deliver to.
  if (looksLikeCommitSha(value)) return t(($) => $.resources.ref_error_commit);
  const validation = validateGitRef(value);
  if (validation.ok) return null;
  return grammarErrorMessage(validation.reason, t);
}

function grammarErrorMessage(
  reason: GitRefInvalidReason,
  t: ReturnType<typeof useT<"projects">>["t"],
): string {
  switch (reason) {
    case "too_long":
      return t(($) => $.resources.ref_error_too_long);
    case "invalid_characters":
      return t(($) => $.resources.ref_error_characters);
    case "invalid_format":
      return t(($) => $.resources.ref_error_format);
  }
}
