/**
 * Checkout-ref helpers for `github_repo` project resources.
 *
 * The ref pins where a project's tasks START — `multica repo checkout` falls
 * back to the remote default branch when it is empty, and an explicit
 * `--ref` on the command line still wins over it. It is not a promise that
 * work lands on that branch, and it does not retarget pull requests.
 *
 * Lives in core (not views) because the mobile app needs the same rules and
 * cannot import from the web-only package.
 */

/**
 * Git's own limit is the filesystem's, but a loose ref path has to fit in a
 * file name. 255 is the conservative ceiling every platform honors, and it is
 * far past any real branch name — the cap exists to stop a paste of an entire
 * document from reaching the database, not to police naming.
 */
export const GIT_REF_MAX_LENGTH = 255;

export type GitRefInvalidReason =
  | "too_long"
  | "invalid_characters"
  | "invalid_format";

export type GitRefValidation =
  | { ok: true }
  | { ok: false; reason: GitRefInvalidReason };

/**
 * Reject input git itself could never resolve, and nothing more.
 *
 * Mirrors the subset of `git check-ref-format` that applies to a ref a user
 * types: it must accept every branch, tag and commit SHA we advertise, so the
 * rules here are about SHAPE only. Whether the ref exists on the remote is a
 * different question answered at checkout time — the daemon may be offline and
 * the repository may be private, so existence must never gate saving config.
 *
 * An empty ref is valid: it means "use the repository's default branch".
 */
export function validateGitRef(ref: string): GitRefValidation {
  const value = ref.trim();
  if (value === "") return { ok: true };
  if (value.length > GIT_REF_MAX_LENGTH) return { ok: false, reason: "too_long" };

  // ASCII control characters, DEL, space, and the characters git reserves for
  // its own revision syntax. Scanned by code point rather than matched with a
  // regex so the control range stays readable (and lintable) as a comparison.
  const RESERVED = "~^:?*[\\";
  for (const char of value) {
    const code = char.codePointAt(0)!;
    if (code <= 0x20 || code === 0x7f || RESERVED.includes(char)) {
      return { ok: false, reason: "invalid_characters" };
    }
  }

  // Path-shape rules. `@{` is reflog syntax; a lone `@` is shorthand for HEAD;
  // `..` would read as a range; `.lock` is what git names its lock files.
  if (
    value.includes("..") ||
    value.includes("@{") ||
    value === "@" ||
    value.startsWith("/") ||
    value.endsWith("/") ||
    value.includes("//") ||
    value.startsWith(".") ||
    value.endsWith(".") ||
    value.endsWith(".lock") ||
    value.split("/").some((segment) => segment.startsWith(".") || segment.endsWith(".lock"))
  ) {
    return { ok: false, reason: "invalid_format" };
  }

  return { ok: true };
}

/**
 * True when the value can only be a commit SHA, never a branch name.
 *
 * Git cannot tell a branch from a tag from a commit by looking at the string —
 * `v1.2.3` is a legal branch name and `main` is a legal tag — and answering
 * properly means asking the remote, which the product deliberately does not do.
 * A full-length hex object id is the one exception: it is unambiguous, and it
 * is what someone pastes when they mean "this exact commit".
 *
 * That matters because a pinned starting point is also the branch a task
 * delivers back to, and a commit has nothing to merge into. So the UI, which
 * asks for a branch, declines this one shape and points at the per-task
 * `--ref` escape hatch instead. The stored grammar (validateGitRef, mirrored
 * server-side) stays permissive: the CLI and API still accept tags and commits,
 * because the daemon resolves all three and always has.
 */
export function looksLikeCommitSha(value: string): boolean {
  const trimmed = value.trim();
  // SHA-1 object ids are 40 hex chars; git's SHA-256 transition uses 64.
  return /^[0-9a-f]{40}$|^[0-9a-f]{64}$/i.test(trimmed);
}

/**
 * Split a pasted GitHub "browse" URL into the clone URL plus the ref it points at.
 *
 * Someone who wants a branch reaches for the URL bar first, and
 * `https://github.com/owner/repo/tree/release/2026-09` is what they copy. Left
 * alone that whole string was stored as the repository URL — a clone target
 * that does not exist. Splitting it is what the user meant.
 *
 * Only `/tree/<ref>` is recognised: `/blob/…` and `/pull/…` point at a file or
 * a PR rather than a checkout baseline, so those are left untouched for the
 * URL validator to reject or accept on its own terms.
 *
 * A multi-segment ref (`release/2026-09`) is rejoined — GitHub's own URLs are
 * ambiguous between `feat/x` the branch and `feat` the branch plus `x` the
 * directory, and the branch reading is the useful one here. A wrong guess is
 * visible and editable in the field before it is saved.
 */
export function splitGithubUrlRef(input: string): { url: string; ref?: string } {
  const value = input.trim();
  const match = value.match(
    /^(https?:\/\/(?:www\.)?github\.com\/[^/\s]+\/[^/\s]+?)(?:\.git)?\/tree\/(.+)$/i,
  );
  const [, cloneUrl, rawRef] = match ?? [];
  if (!cloneUrl || !rawRef) return { url: value };
  const ref = rawRef.replace(/\/+$/, "");
  if (!ref || validateGitRef(ref).ok === false) return { url: value };
  return { url: cloneUrl, ref };
}
