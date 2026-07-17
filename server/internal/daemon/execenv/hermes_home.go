package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// Hermes discovers skills from exactly two places (verified against the bundled
// Hermes agent source, agent/skill_utils.py get_all_skills_dirs): its home
// skills dir `<HERMES_HOME>/skills/` first, then the directories listed under
// `skills.external_dirs` in `<HERMES_HOME>/config.yaml`, in config order. It has
// NO workspace-relative discovery, so the generic `.agent_context/skills/`
// fallback the daemon used before was never scanned and workspace-assigned
// skills silently never took effect (issue #5242).
//
// Rather than replace HERMES_HOME with a home rebuilt from a fixed allowlist —
// which would silently change behavior for tasks that don't even use skills and
// would drop any home state not on the list (plugins, OAuth state, hooks, SOUL,
// scripts, and whatever Hermes adds next) — this builds a minimal compatibility
// overlay, and only when the agent actually has skills bound (gated at the call
// site in Prepare/Reuse; a skill-less Hermes task keeps HERMES_HOME untouched).
//
// The overlay:
//   - mirrors every top-level entry of the shared ~/.hermes/ into the per-task
//     home via symlink, EXCEPT the entries it overrides — so the denylist stays
//     tiny and future home state is inherited automatically instead of being
//     missed by an allowlist;
//   - derives a task-local config.yaml whose `skills.external_dirs` points at
//     the shared skills dir plus the user's existing external_dirs, expanded
//     against the agent's effective env and normalized to absolute paths (Hermes
//     resolves relative external_dirs against HERMES_HOME, so leaving them
//     relative after the redirect would silently repoint them) — this exposes
//     the user's global/builtin skills read-only without copying them;
//   - populates the task-local `skills/` dir with ONLY the Multica-bound skills,
//     which take precedence because Hermes lists the home skills dir first;
//   - keeps `memories/` overlay-owned (a fresh per-task dir), NOT symlinked to
//     the shared home: Hermes loads and writes back MEMORY.md/USER.md there, and
//     per-agent memory is a Multica product concern — the host's local Hermes
//     memory must not bleed into a task, nor task memory back out to the host;
//   - keeps the state.db SQLite session store and its journal sidecars
//     overlay-owned too: Hermes creates them lazily, Reuse preserves them for
//     the task, and host conversation history is never linked or copied;
//   - disables the external `memory.provider` in the derived config so a
//     host-configured Supermemory/Hindsight/etc. backend isn't shared across
//     tasks. This is the on-disk + external-backend memory isolation; a managed,
//     agent-scoped memory backend is a separate future product decision.
//
// The shared ~/.hermes/ is never modified by this setup. Note however that
// mirrored entries are writable symlinks, so if Hermes writes through one at
// runtime (e.g. refreshing token state under a mirrored auth/OAuth path) that
// write does reach the shared home — that propagation is intentional.

// hermesOverriddenEntries are the fixed top-level entries of the shared
// ~/.hermes/ that the overlay supplies its own task-local version of, so they
// are NOT mirrored from the shared home and are preserved across reuse
// reconciliation:
//   - skills/       task-local, only the bound skills
//   - config.yaml   derived config with absolutized external_dirs
//   - memories/     fresh per-task dir, isolated from the host's memory
//   - marker below  records that legacy shared SQLite state was detached
//
// The state.db SQLite family is classified dynamically by
// isHermesOverlayOwnedEntry so every journal sidecar stays task-local too.
// Everything else in the shared home is mirrored generically.
//
// active_profile and profiles are also overlay-owned (never mirrored): Hermes
// reads <HERMES_HOME>/active_profile at startup and, if it names a non-default
// profile, redirects HERMES_HOME to that real profile — which would bypass the
// overlay's skills and memory isolation. Keeping active_profile out of the
// overlay means Hermes finds none and stays put. The overlay is already seeded
// from the correct (possibly profile) source home.
//
// .env is overlay-owned too, but unlike the others it is DERIVED, not just
// omitted (see writeDerivedHermesEnv): Hermes runs _apply_profile_override()
// and then load_hermes_dotenv(), which loads <HERMES_HOME>/.env with
// override=True. A source .env carrying an out-of-band HERMES_HOME= would
// overwrite the overlay's HERMES_HOME after the argv/sticky-profile protections
// ran, repointing skill discovery and memory back at the source home. The
// derived overlay .env preserves the source's credentials/settings but pins
// HERMES_HOME to the overlay, and is written even when the source has none so
// Hermes' project-.env fallback (loaded with override=True only when no user
// .env loaded) can't relocate the home either.
const hermesTaskLocalStateMarker = ".multica-task-local-state-v1"

var hermesOverriddenEntries = map[string]struct{}{
	"skills":                   {},
	"config.yaml":              {},
	"memories":                 {},
	"active_profile":           {},
	"profiles":                 {},
	".env":                     {},
	hermesTaskLocalStateMarker: {},
}

// isHermesOverlayOwnedEntry reports whether name belongs to the per-task
// overlay rather than the shared Hermes home. Hermes' state.db is the canonical
// session store and uses WAL mode; mirroring its main file and journal sidecars
// separately can produce an inconsistent snapshot, exposes host conversation
// history, and fails on Windows when SQLite byte-range-locks state.db-shm.
func isHermesOverlayOwnedEntry(name string) bool {
	if _, owned := hermesOverriddenEntries[name]; owned {
		return true
	}
	return isHermesTaskLocalStateEntry(name)
}

func isHermesTaskLocalStateEntry(name string) bool {
	return name == "state.db" || strings.HasPrefix(name, "state.db-")
}

// platformDefaultHermesHome returns Hermes' platform-native default home:
// %LOCALAPPDATA%\hermes on native Windows, ~/.hermes elsewhere — matching
// hermes_constants._get_platform_default_hermes_home. Without the Windows branch
// a Windows user with no explicit HERMES_HOME would seed the overlay from an
// empty ~/.hermes and lose their real config/auth/global skills.
func platformDefaultHermesHome() string {
	la, _ := os.LookupEnv("LOCALAPPDATA")
	home, _ := os.UserHomeDir()
	return platformDefaultHermesHomeFor(runtime.GOOS, la, home)
}

// platformDefaultHermesHomeFor is the pure core of platformDefaultHermesHome,
// split out so the Windows branch is testable off a Windows host. It matches
// hermes_constants._get_platform_default_hermes_home: on Windows the base is
// %LOCALAPPDATA%, or %USERPROFILE%\AppData\Local when LOCALAPPDATA is unset,
// with `hermes` appended; POSIX uses ~/.hermes.
func platformDefaultHermesHomeFor(goos, localAppData, userHome string) string {
	if goos == "windows" {
		base := strings.TrimSpace(localAppData)
		if base == "" && userHome != "" {
			base = filepath.Join(userHome, "AppData", "Local")
		}
		if base != "" {
			return filepath.Join(base, "hermes")
		}
	}
	if userHome != "" {
		return filepath.Join(userHome, ".hermes")
	}
	return filepath.Join(os.TempDir(), ".hermes") // last-resort fallback
}

// hermesProfileNameRe mirrors Hermes' hermes_cli.profiles._PROFILE_ID_RE — the
// shape a profile identifier must have on disk and in argv.
var hermesProfileNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// hermesReservedProfileNames mirrors hermes_cli.profiles._RESERVED_NAMES: names
// Hermes' validate_profile_name rejects (they would collide with the install
// itself or a common system binary). "default" is in Hermes' set too but is a
// special pass-through there — it names the root home — so it is handled before
// this check, not listed here.
var hermesReservedProfileNames = map[string]struct{}{
	"hermes": {}, "test": {}, "tmp": {}, "root": {}, "sudo": {},
}

// HermesProfileResolution is the single authoritative result of resolving a
// Hermes profile selection: the source home to seed the overlay from (and to
// expand ${HERMES_HOME} against), whether that home must already exist, and a
// non-nil Err when the selection is one Hermes would refuse to start under.
type HermesProfileResolution struct {
	// SourceHome is the resolved HERMES_HOME the overlay is built from. It is
	// also the value ${HERMES_HOME} in a profile's skills.external_dirs expands
	// to, matching native Hermes applying the profile override before it loads
	// config.yaml.
	SourceHome string
	// MustExist fails the overlay closed when SourceHome is absent — set for a
	// named/profile-scoped source so a typo doesn't silently seed from an empty
	// dir and drop the user's auth/config, matching Hermes' own FileNotFoundError
	// sys.exit on a missing profile.
	MustExist bool
	// Err is set when the selection names a reserved or otherwise invalid
	// profile (including the empty inline `--profile=` value). Hermes sys.exit(1)s
	// in these cases, so the daemon must fail the task closed rather than start
	// it under the default profile.
	Err error
}

// ResolveHermesProfile is the one resolver contract for Hermes profile
// selection. Given the agent's custom_env HERMES_HOME and the profile selection
// already parsed from custom_args (agent.ParseHermesProfileArgs), it reproduces
// hermes_cli.main._apply_profile_override + hermes_cli.profiles semantics:
//
//   - The Hermes root is derived exactly like get_default_hermes_root: an
//     explicit custom_env HERMES_HOME, else the process HERMES_HOME, else the
//     platform default; if that home is itself <root>/profiles/<name>, the root
//     is <root> (profiles are always resolved against the root, never nested).
//   - An explicit -p/--profile wins. Otherwise an already-profile-scoped
//     HERMES_HOME is trusted as-is (step 1.5), and only failing that is the
//     sticky <root>/active_profile consulted (step 2).
//   - The chosen name is normalized + validated like normalize_profile_name /
//     validate_profile_name: "default" (case-insensitively) means the root home;
//     an empty, malformed, or reserved name is a hard error (Err set).
//   - A valid named profile resolves to <root>/profiles/<name> and MustExist.
//
// found/inline come from the parsed selection: found means an explicit flag with
// a value matched; inline distinguishes the `--profile=<value>` form, whose empty
// value must hard-fail rather than fall back to the default.
func ResolveHermesProfile(customEnvHome, name string, found, inline bool) HermesProfileResolution {
	base := strings.TrimSpace(customEnvHome)
	if base == "" {
		base = strings.TrimSpace(os.Getenv("HERMES_HOME"))
	}
	if base == "" {
		base = platformDefaultHermesHome()
	}
	if abs, err := filepath.Abs(base); err == nil {
		base = abs
	}
	root := hermesRootFromHome(base)

	profile := name
	if !found {
		// Step 1.5: trust an already-profile-scoped HERMES_HOME (immediate parent
		// dir named "profiles") without consulting active_profile.
		if base != "" && filepath.Base(filepath.Dir(base)) == "profiles" {
			return HermesProfileResolution{SourceHome: base, MustExist: true}
		}
		// Step 2: honor the sticky <root>/active_profile. (The container-only
		// HERMES_S6_SUPERVISED_CHILD exception in Hermes does not apply to a
		// daemon task spawn.) When no sticky applies, the base home (the
		// root/default) is the source.
		profile = readHermesActiveProfile(root)
		if profile == "" {
			return HermesProfileResolution{SourceHome: base}
		}
	}

	// An explicit selection (found) is always validated — an empty inline value
	// (`--profile=`) is a hard error, not a fall-back to the default — as is a
	// sticky name, matching Hermes calling resolve_profile_env on both.
	home, mustExist, err := hermesProfileDir(root, profile)
	if err != nil {
		return HermesProfileResolution{Err: err}
	}
	return HermesProfileResolution{SourceHome: home, MustExist: mustExist}
}

// hermesRootFromHome reproduces hermes_constants.get_default_hermes_root: the
// root for profile-level operations. If base is the platform default or lives
// under it (normal or profile mode) the root is the platform default; otherwise
// (Docker/custom home) a <...>/profiles/<name> layout roots at the grandparent,
// and any other path is its own root.
func hermesRootFromHome(base string) string {
	return hermesRootFromHomeFor(base, platformDefaultHermesHome())
}

// hermesRootFromHomeFor is the pure core of hermesRootFromHome with the native
// home injected for testability. The containment test resolves symlinks on both
// sides (like get_default_hermes_root's env_path.resolve().relative_to(
// native_home.resolve())), so a HERMES_HOME symlinked into <native>/profiles/<x>
// still roots at native. The RETURNED value stays unresolved, matching Hermes,
// which returns native_home / the lexical grandparent of the original env_path.
func hermesRootFromHomeFor(base, native string) string {
	if base == "" {
		return native
	}
	if isPathUnder(resolvePathBestEffort(native), resolvePathBestEffort(base)) {
		return native
	}
	if filepath.Base(filepath.Dir(base)) == "profiles" {
		return filepath.Dir(filepath.Dir(base))
	}
	return base
}

// resolvePathBestEffort resolves symlinks like Python's Path.resolve(strict=False):
// it follows every symlink in the existing prefix of p and appends the remaining
// non-existent tail unchanged, rather than failing (as filepath.EvalSymlinks does)
// when p doesn't fully exist. The result is absolute.
func resolvePathBestEffort(p string) string {
	if p == "" {
		return p
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	dir := p
	var tail []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return p // reached the root without an existing ancestor
		}
		tail = append([]string{filepath.Base(dir)}, tail...)
		dir = parent
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(append([]string{resolved}, tail...)...)
		}
	}
}

// isPathUnder reports whether child is parent or nested under it.
func isPathUnder(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// readHermesActiveProfile returns the sticky profile name from
// <root>/active_profile, or "" when absent, unreadable, empty, or "default"
// (matching _apply_profile_override step 2, which ignores a "default" sticky).
func readHermesActiveProfile(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "active_profile"))
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(data))
	if name == "default" {
		return ""
	}
	return name
}

// hermesProfileDir resolves a profile name against root, reproducing
// normalize_profile_name + validate_profile_name + get_profile_dir. It returns
// the home dir, whether that home must already exist (true for a named profile),
// or an error for an empty/malformed/reserved name (which Hermes sys.exit(1)s on).
func hermesProfileDir(root, name string) (home string, mustExist bool, err error) {
	stripped := strings.TrimSpace(name)
	if stripped == "" {
		return "", false, fmt.Errorf("hermes profile name cannot be empty")
	}
	var canon string
	if strings.EqualFold(stripped, "default") {
		canon = "default"
	} else {
		canon = strings.ToLower(stripped)
	}
	if canon == "default" {
		return root, false, nil // the default profile IS the root home
	}
	if !hermesProfileNameRe.MatchString(canon) {
		return "", false, fmt.Errorf("invalid hermes profile name %q", canon)
	}
	if _, reserved := hermesReservedProfileNames[canon]; reserved {
		return "", false, fmt.Errorf("hermes profile name %q is reserved", canon)
	}
	return filepath.Join(root, "profiles", canon), true, nil
}

// prepareHermesHome builds the per-task HERMES_HOME compatibility overlay
// described above. The daemon exports the given path as HERMES_HOME on the
// hermes subprocess so the CLI discovers the bound skills natively.
//
// Callers gate this on the agent having skills bound; it is a full rebuild each
// time (mirror reconciled, config re-derived, bound skills rewritten) so a Reuse
// after a skill/config change lands cleanly. It fails CLOSED: if the mirror, the
// derived config, or the bound skills can't be established the whole overlay is
// unusable, so the error propagates and the caller must not start Hermes against
// a half-built home.
// sourceHome is the shared home to seed from (resolved by the daemon via
// ResolveHermesProfile, honoring the agent's HERMES_HOME/profile); empty
// falls back to the platform default. sourceMustExist fails closed when the
// source home is absent — set for an explicitly named profile so a typo doesn't
// silently seed from an empty dir and drop the user's auth/config. env is the
// sanitized effective env used to expand ${VAR} in external_dirs so it matches
// what the Hermes child sees.
func prepareHermesHome(hermesHome, sourceHome string, sourceMustExist bool, workspaceSkills []SkillContextForEnv, env map[string]string, logger *slog.Logger) error {
	sharedHome := strings.TrimSpace(sourceHome)
	if sharedHome == "" {
		sharedHome = platformDefaultHermesHome()
	}
	if sourceMustExist {
		if fi, err := os.Stat(sharedHome); err != nil || !fi.IsDir() {
			return fmt.Errorf("hermes profile home %q not found (create it with `hermes profile create`)", sharedHome)
		}
	}

	if err := os.MkdirAll(hermesHome, 0o700); err != nil {
		return fmt.Errorf("create hermes-home dir: %w", err)
	}
	// Tighten perms on reuse too — MkdirAll leaves an existing dir's mode alone,
	// and the derived config below can hold inline api_key secrets.
	if err := os.Chmod(hermesHome, 0o700); err != nil {
		return fmt.Errorf("chmod hermes-home dir: %w", err)
	}
	if err := prepareHermesTaskLocalState(hermesHome); err != nil {
		return fmt.Errorf("prepare task-local state: %w", err)
	}
	// Fresh, isolated per-task memories dir (idempotent — preserved across reuse
	// so the task/issue lifecycle keeps its own memory).
	if err := os.MkdirAll(filepath.Join(hermesHome, "memories"), 0o700); err != nil {
		return fmt.Errorf("create task memories dir: %w", err)
	}

	if err := mirrorSharedHermesHome(sharedHome, hermesHome, logger); err != nil {
		return fmt.Errorf("mirror shared hermes home: %w", err)
	}
	if err := writeDerivedHermesConfig(sharedHome, hermesHome, env, logger); err != nil {
		return fmt.Errorf("derive hermes config: %w", err)
	}
	if err := writeDerivedHermesEnv(sharedHome, hermesHome); err != nil {
		return fmt.Errorf("derive hermes .env: %w", err)
	}
	return writeHermesBoundSkills(hermesHome, workspaceSkills, logger)
}

// writeDerivedHermesEnv writes the task-local .env: the source home's .env
// contents (credentials/settings preserved) with any HERMES_HOME assignment
// removed, then a pinned HERMES_HOME pointing at the overlay appended last so it
// wins. Hermes loads <HERMES_HOME>/.env with override=True right after profile
// resolution, so without this an out-of-band HERMES_HOME= in the source .env
// would relocate the home past the overlay (dropping bound skills and memory
// isolation). We always write the file — even when the source has none — so the
// overlay .env "loads" and Hermes' project-.env fallback (override=True only when
// no user .env loaded) can't relocate the home either. Written 0600 via atomic
// replace since it can hold API-key secrets; reuse also repairs prior perms.
func writeDerivedHermesEnv(sharedHome, hermesHome string) error {
	dst := filepath.Join(hermesHome, ".env")

	var body []byte
	src, err := os.ReadFile(filepath.Join(sharedHome, ".env"))
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read shared .env: %w", err)
		}
	} else {
		body = stripDotenvAssignment(src, "HERMES_HOME")
	}

	var buf strings.Builder
	if len(body) > 0 {
		buf.Write(body)
		if body[len(body)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	// Pin HERMES_HOME to the overlay. Single-quote the value so python-dotenv
	// treats it literally (no escaping / var expansion) — task home paths can
	// contain spaces or other characters under the workspaces root.
	fmt.Fprintf(&buf, "HERMES_HOME='%s'\n", hermesHome)

	return writeFileAtomic(dst, []byte(buf.String()), 0o600)
}

// stripDotenvAssignment drops every line of a .env file that assigns key,
// tolerating a leading `export ` and surrounding whitespace the way python-dotenv
// does. Other lines (including comments and blanks) are preserved verbatim.
func stripDotenvAssignment(content []byte, key string) []byte {
	lines := strings.Split(string(content), "\n")
	out := lines[:0]
	for _, line := range lines {
		if dotenvLineKey(line) == key {
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

// dotenvLineKey returns the variable name a .env line assigns, or "" for a
// comment/blank/non-assignment line.
func dotenvLineKey(line string) string {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") {
		return ""
	}
	if rest := strings.TrimPrefix(s, "export"); rest != s && rest != "" &&
		(rest[0] == ' ' || rest[0] == '\t') {
		s = strings.TrimSpace(rest)
	}
	eq := strings.IndexByte(s, '=')
	if eq <= 0 {
		return ""
	}
	return strings.TrimSpace(s[:eq])
}

// mirrorSharedHermesHome symlinks every top-level entry of the shared ~/.hermes/
// into the per-task home except the overlay-owned entries, then reconciles the
// destination so entries removed from the shared home (or left over from a prior
// reuse) don't linger as readable stale state. Symlinks share state with the
// user's real home (auth/OAuth refreshes propagate, no credential copy lingers
// in task scratch). The shared home itself is never written — we only read it
// and create links pointing into it.
func mirrorSharedHermesHome(sharedHome, hermesHome string, logger *slog.Logger) error {
	entries, err := os.ReadDir(sharedHome)
	if err != nil {
		if os.IsNotExist(err) {
			// No shared home to mirror. The derived config + bound skills still
			// give Hermes a working home, so this is not fatal on its own.
			return reconcileMirroredEntries(hermesHome, nil)
		}
		return fmt.Errorf("read shared home: %w", err)
	}
	mirrored := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if isHermesOverlayOwnedEntry(name) {
			continue
		}
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(hermesHome, name)
		if err := linkSharedHermesEntry(src, dst); err != nil {
			return fmt.Errorf("mirror %s: %w", name, err)
		}
		mirrored[name] = struct{}{}
	}
	return reconcileMirroredEntries(hermesHome, mirrored)
}

// reconcileMirroredEntries removes overlay entries that are neither overlay-owned
// nor currently mirrored from the shared home, so a shared entry deleted between
// runs (or a Windows copy-fallback left behind) doesn't survive as stale state.
func reconcileMirroredEntries(hermesHome string, mirrored map[string]struct{}) error {
	entries, err := os.ReadDir(hermesHome)
	if err != nil {
		return fmt.Errorf("read overlay home: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if isHermesOverlayOwnedEntry(name) {
			continue
		}
		if _, keep := mirrored[name]; keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(hermesHome, name)); err != nil {
			return fmt.Errorf("reconcile stale %s: %w", name, err)
		}
	}
	return nil
}

// prepareHermesTaskLocalState migrates an overlay built by an older daemon away
// from the shared Hermes SQLite session store. Without the marker, state.db and
// its sidecars may be symlinks or independently copied files; neither is safe to
// reuse as task-local state. Remove only those entries inside the generated
// overlay, then record the migration atomically. Hermes lazily creates a fresh
// database, and later prepares preserve it because the marker is present.
func prepareHermesTaskLocalState(hermesHome string) error {
	marker := filepath.Join(hermesHome, hermesTaskLocalStateMarker)
	if fi, err := os.Lstat(marker); err == nil {
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("state marker is not a regular file: %s", marker)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat state marker: %w", err)
	}

	entries, err := os.ReadDir(hermesHome)
	if err != nil {
		return fmt.Errorf("read overlay home: %w", err)
	}
	for _, entry := range entries {
		if !isHermesTaskLocalStateEntry(entry.Name()) {
			continue
		}
		path := filepath.Join(hermesHome, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove legacy task state %s: %w", path, err)
		}
	}
	return writeFileAtomic(marker, []byte("task-local Hermes state\n"), 0o600)
}

// linkSharedHermesEntry symlinks dst → src, idempotent across Reuse: an existing
// link already pointing at src is left alone; anything else is removed and
// recreated so the overlay never drifts from the shared home. A dangling source
// (a broken symlink in the user's home) is skipped, not failed. Directories use
// createDirLink and files createFileLink so the Windows copy fallbacks match the
// entry kind.
func linkSharedHermesEntry(src, dst string) error {
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Readlink(dst); err == nil && target == src {
				return nil
			}
		}
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("remove stale %s: %w", dst, err)
		}
	}

	info, err := os.Stat(src) // follow the link to decide dir vs file
	if err != nil {
		if os.IsNotExist(err) {
			return nil // dangling source in the user's home — nothing to link
		}
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if info.IsDir() {
		return createDirLink(src, dst)
	}
	return createFileLink(src, dst)
}

// writeDerivedHermesConfig writes the task-local config.yaml: the user's config
// with `skills.external_dirs` set to their existing external dirs plus the shared
// ~/.hermes/skills, all as absolute paths. When the user has no config we still
// write a minimal one so their global skills stay reachable via the external
// root. If the config can't be parsed we copy it verbatim so auth/model settings
// survive — the bound skills still load from the task-local skills/ dir, which is
// the point of the fix; only the user's global skills would be missing. The file
// is written 0600 (it can hold inline api_key secrets) via atomic replace, so
// reuse also repairs a prior file's permissions.
func writeDerivedHermesConfig(sharedHome, hermesHome string, env map[string]string, logger *slog.Logger) error {
	srcConfig := filepath.Join(sharedHome, "config.yaml")
	dstConfig := filepath.Join(hermesHome, "config.yaml")

	data, err := os.ReadFile(srcConfig)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read shared config: %w", err)
		}
		doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
		if err := setHermesExternalDirs(doc, computeHermesExternalDirs(sharedHome, nil, env)); err != nil {
			return err
		}
		return marshalYAMLToFile(doc, dstConfig)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		logger.Warn("execenv: hermes-home config parse failed; copying verbatim", "error", err)
		return writeFileAtomic(dstConfig, data, 0o600)
	}
	dirs := computeHermesExternalDirs(sharedHome, existingHermesExternalDirs(&doc), env)
	if err := setHermesExternalDirs(&doc, dirs); err != nil {
		logger.Warn("execenv: hermes-home set external_dirs failed; copying verbatim", "error", err)
		return writeFileAtomic(dstConfig, data, 0o600)
	}
	// Disable any host-configured external memory backend (memory.provider) so a
	// Supermemory/Hindsight/etc. bank isn't shared across managed tasks; the
	// built-in per-task memories/ dir is already isolated above.
	disableHermesMemoryProvider(&doc)
	return marshalYAMLToFile(&doc, dstConfig)
}

// disableHermesMemoryProvider forces skills-adjacent `memory.provider` to empty
// in the derived config. Hermes activates an external memory plugin only when
// memory.provider is a non-blank string (agent/agent_init.py), so "" is the
// explicit off switch. The built-in note/user-profile memory is unaffected — it
// writes to the isolated per-task memories/ dir.
func disableHermesMemoryProvider(doc *yaml.Node) {
	top := yamlDocumentRoot(doc)
	if top == nil {
		return
	}
	memory := yamlMapValue(top, "memory")
	if memory == nil || memory.Kind != yaml.MappingNode {
		memory = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		yamlSetMapValue(top, "memory", memory)
	}
	yamlSetMapValue(memory, "provider", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""})
}

// computeHermesExternalDirs normalizes the user's existing external_dirs to
// absolute paths and appends the shared skills dir as a read-only external root.
// Variable/`~` expansion uses the sanitized effective env (the same map layered
// onto the Hermes child), falling back to the daemon process env — so a `${VAR}`
// configured on the agent resolves to what Hermes will actually see, and a var
// the daemon blocklists (e.g. HOME) resolves to the process value rather than
// the dropped custom one. Unknown variables are PRESERVED as `${VAR}` (matching
// Hermes/Python expandvars) rather than collapsed to empty, so a path meant to
// be resolved at runtime isn't silently rewritten. An entry still containing an
// unresolved `${` is left as-is (Hermes expands it later); otherwise relative
// paths resolve against the shared home, matching pre-redirect behavior. Order
// preserved; duplicates dropped.
func computeHermesExternalDirs(sharedHome string, existing []string, env map[string]string) []string {
	expand := func(s string) string {
		return os.Expand(s, func(k string) string {
			if v, ok := env[k]; ok {
				return v
			}
			if v, ok := os.LookupEnv(k); ok {
				return v
			}
			return "${" + k + "}" // preserve unknown vars for runtime expansion
		})
	}

	out := make([]string, 0, len(existing)+1)
	seen := make(map[string]struct{}, len(existing)+1)
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	for _, raw := range existing {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		entry = strings.TrimSpace(expand(entry))
		if entry == "" {
			continue
		}
		// An unresolved ${VAR} remains (unknown var preserved above) — leave the
		// entry untouched so Hermes expands and resolves it at runtime; we can't
		// safely decide abs-vs-relative here.
		if strings.Contains(entry, "${") {
			add(entry)
			continue
		}
		if entry == "~" || strings.HasPrefix(entry, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				entry = filepath.Join(home, strings.TrimPrefix(entry, "~"))
			}
		}
		if !filepath.IsAbs(entry) {
			entry = filepath.Join(sharedHome, entry)
		}
		add(filepath.Clean(entry))
	}
	// The shared skills dir, referenced (not copied) so the user's global and
	// builtin skills stay visible. It differs from the task-local skills dir,
	// so Hermes won't fold it into the local root, and being last it yields to
	// the bound skills on a name collision.
	add(filepath.Join(sharedHome, "skills"))
	return out
}

// writeHermesBoundSkills rebuilds the task-local skills/ dir from scratch so a
// skill removed since the last run can't linger, then writes only the
// Multica-bound skills. They keep their natural slug (no user skills share this
// dir) and therefore take precedence over any same-named external skill.
func writeHermesBoundSkills(hermesHome string, workspaceSkills []SkillContextForEnv, logger *slog.Logger) error {
	skillsDir := filepath.Join(hermesHome, "skills")
	if err := os.RemoveAll(skillsDir); err != nil {
		return fmt.Errorf("clear hermes skills dir: %w", err)
	}
	if len(workspaceSkills) == 0 {
		// Defensive: callers gate on a non-empty set, but stay correct if that
		// ever changes — an empty local dir just means the external root is the
		// only source, matching un-redirected behavior.
		return os.MkdirAll(skillsDir, 0o700)
	}
	// Skills live under env.RootDir/hermes-home, which the GC loop (cloud) or
	// env teardown (local_directory) wipes wholesale — no sidecar manifest.
	return writeSkillFiles(skillsDir, workspaceSkills, nil)
}

// existingHermesExternalDirs reads the raw skills.external_dirs entries from a
// parsed config document, accepting either a single string or a list (both of
// which Hermes accepts). Returns nil when absent.
func existingHermesExternalDirs(doc *yaml.Node) []string {
	top := yamlDocumentRoot(doc)
	if top == nil {
		return nil
	}
	skills := yamlMapValue(top, "skills")
	ed := yamlMapValue(skills, "external_dirs")
	if ed == nil {
		return nil
	}
	if ed.Kind == yaml.ScalarNode {
		return []string{ed.Value}
	}
	if ed.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(ed.Content))
	for _, c := range ed.Content {
		if c.Kind == yaml.ScalarNode {
			out = append(out, c.Value)
		}
	}
	return out
}

// setHermesExternalDirs sets skills.external_dirs on the config document,
// creating the skills mapping if needed and preserving every other setting.
func setHermesExternalDirs(doc *yaml.Node, dirs []string) error {
	top := yamlDocumentRoot(doc)
	if top == nil {
		return fmt.Errorf("hermes config: unexpected root node")
	}
	skills := yamlMapValue(top, "skills")
	if skills == nil || skills.Kind != yaml.MappingNode {
		skills = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		yamlSetMapValue(top, "skills", skills)
	}
	yamlSetMapValue(skills, "external_dirs", yamlStringSeq(dirs))
	return nil
}

// yamlDocumentRoot returns the top-level mapping node of a parsed document, or
// nil if the shape isn't a mapping.
func yamlDocumentRoot(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	node := doc
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}

// yamlMapValue returns the value node for key in a mapping node, or nil.
func yamlMapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// yamlSetMapValue sets key to val in a mapping node, replacing in place if the
// key exists or appending otherwise.
func yamlSetMapValue(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		val,
	)
}

// yamlStringSeq builds a YAML sequence node of string scalars.
func yamlStringSeq(vals []string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, v := range vals {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
	}
	return seq
}

// marshalYAMLToFile renders a YAML node to dst as a 0600 file (it can hold
// inline secrets) via atomic replace.
func marshalYAMLToFile(doc *yaml.Node, dst string) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal hermes config: %w", err)
	}
	return writeFileAtomic(dst, out, 0o600)
}

// writeFileAtomic writes data to a temp file in the destination directory with
// the given perms, then renames it over dst — so readers never see a partial
// file and a prior file's looser permissions are replaced.
func writeFileAtomic(dst string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".hermes-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", dst, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp for %s: %w", dst, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp for %s: %w", dst, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", dst, err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("rename temp to %s: %w", dst, err)
	}
	return nil
}
