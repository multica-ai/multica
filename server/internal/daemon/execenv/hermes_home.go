package execenv

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// Hermes only discovers managed skills under HERMES_HOME. Issue tasks therefore
// use a task-private home containing the bound Multica skills and a narrow,
// immutable projection of runtime state needed to launch Hermes. The projection
// never links back to daemon-owner paths, never exposes owner skills or
// external_dirs, and drops unknown current or future owner-home entries.
//
// auth.json, oauth_state.json, .env, and config.yaml are stable bounded file
// snapshots or derived documents. plugins/ is a bounded fail-closed directory
// snapshot. memories/ and skills/ are task-owned. Profile selectors are absent
// so Hermes cannot redirect itself back to an owner home after launch.
var hermesTaskHomeEntries = map[string]struct{}{
	"auth.json":        {},
	"oauth_state.json": {},
	"plugins":          {},
	"skills":           {},
	"config.yaml":      {},
	"memories":         {},
	".env":             {},
}

const (
	hermesRuntimeSnapshotMaxBytes    = 16 << 20
	hermesPluginSnapshotMaxFiles     = 10_000
	hermesPluginSnapshotMaxFileBytes = 16 << 20
	hermesPluginSnapshotMaxBytes     = 256 << 20
)

var (
	errUnsafeHermesPluginEntry = errors.New("unsafe Hermes plugin entry")
	errHermesPluginLimit       = errors.New("Hermes plugin snapshot limit exceeded")
)

// hermesOwnerEnvAllowlist is the complete set of source-home dotenv variables
// that may be projected into an issue task. Keep this explicit: the owner .env
// can contain unrelated process settings and credentials that the task must not
// inherit merely because it needs a Hermes skills overlay.
var hermesOwnerEnvAllowlist = map[string]struct{}{
	"ALIBABA_CODING_PLAN_API_KEY":  {},
	"ALIBABA_CODING_PLAN_BASE_URL": {},
	"ANTHROPIC_API_KEY":            {},
	"ANTHROPIC_BASE_URL":           {},
	"ANTHROPIC_TOKEN":              {},
	"ARCEEAI_API_KEY":              {},
	"ARCEE_BASE_URL":               {},
	"AZURE_FOUNDRY_API_KEY":        {},
	"AZURE_FOUNDRY_BASE_URL":       {},
	"BEDROCK_BASE_URL":             {},
	"CLAUDE_CODE_OAUTH_TOKEN":      {},
	"COPILOT_ACP_BASE_URL":         {},
	"COPILOT_API_BASE_URL":         {},
	"DASHSCOPE_API_KEY":            {},
	"DASHSCOPE_BASE_URL":           {},
	"DEEPSEEK_API_KEY":             {},
	"DEEPSEEK_BASE_URL":            {},
	"GEMINI_API_KEY":               {},
	"GEMINI_BASE_URL":              {},
	"GLM_API_KEY":                  {},
	"GLM_BASE_URL":                 {},
	"GMI_API_KEY":                  {},
	"GMI_BASE_URL":                 {},
	"GOOGLE_API_KEY":               {},
	"HF_BASE_URL":                  {},
	"HF_TOKEN":                     {},
	"KILOCODE_API_KEY":             {},
	"KILOCODE_BASE_URL":            {},
	"KIMI_API_KEY":                 {},
	"KIMI_BASE_URL":                {},
	"KIMI_CN_API_KEY":              {},
	"KIMI_CODING_API_KEY":          {},
	"LM_API_KEY":                   {},
	"LM_BASE_URL":                  {},
	"MINIMAX_API_KEY":              {},
	"MINIMAX_BASE_URL":             {},
	"MINIMAX_CN_API_KEY":           {},
	"MINIMAX_CN_BASE_URL":          {},
	"NVIDIA_API_KEY":               {},
	"NVIDIA_BASE_URL":              {},
	"OLLAMA_API_KEY":               {},
	"OLLAMA_BASE_URL":              {},
	"OPENCODE_GO_API_KEY":          {},
	"OPENCODE_GO_BASE_URL":         {},
	"OPENCODE_ZEN_API_KEY":         {},
	"OPENCODE_ZEN_BASE_URL":        {},
	"OPENAI_API_KEY":               {},
	"OPENAI_BASE_URL":              {},
	"OPENROUTER_API_KEY":           {},
	"OPENROUTER_BASE_URL":          {},
	"STEPFUN_API_KEY":              {},
	"STEPFUN_BASE_URL":             {},
	"TOKENHUB_API_KEY":             {},
	"TOKENHUB_BASE_URL":            {},
	"XAI_API_KEY":                  {},
	"XAI_BASE_URL":                 {},
	"XIAOMI_API_KEY":               {},
	"XIAOMI_BASE_URL":              {},
	"ZAI_API_KEY":                  {},
	"Z_AI_API_KEY":                 {},
}

// hermesOwnerModelConfigAllowlist contains the public runtime fields accepted
// from Hermes' modern model mapping. Credential-bearing and unknown model
// fields are intentionally omitted.
var hermesOwnerModelConfigAllowlist = []string{
	"default",
	"model",
	"provider",
	"base_url",
	"api_mode",
	"openai_runtime",
	"auth_mode",
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

// prepareHermesHome builds the task-private HERMES_HOME described above. The
// daemon exports the given path as HERMES_HOME so Hermes discovers only the
// task-bound skills and projected runtime state.
//
// Callers gate this on the agent having skills bound; it is a full rebuild each
// time (state re-snapshotted, config re-derived, bound skills rewritten) so a
// Reuse after a skill/config change lands cleanly. It fails CLOSED: if any
// projection cannot be established, the caller must not start Hermes against a
// partial home.
// sourceHome is the shared home to seed from (resolved by the daemon via
// ResolveHermesProfile, honoring the agent's HERMES_HOME/profile); empty
// falls back to the platform default. sourceMustExist fails closed when the
// source home is absent — set for an explicitly named profile so a typo doesn't
// silently seed from an empty dir and drop the user's auth/config. env is kept
// in the signature for compatibility with existing callers; owner external_dirs
// are intentionally not projected.
func prepareHermesHome(hermesHome, sourceHome string, sourceMustExist bool, workspaceSkills []SkillContextForEnv, _ map[string]string, logger *slog.Logger) error {
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
	// and the derived files below contain projected owner runtime settings.
	if err := os.Chmod(hermesHome, 0o700); err != nil {
		return fmt.Errorf("chmod hermes-home dir: %w", err)
	}
	// Fresh, isolated per-task memories dir (idempotent — preserved across reuse
	// so the task/issue lifecycle keeps its own memory).
	if err := os.MkdirAll(filepath.Join(hermesHome, "memories"), 0o700); err != nil {
		return fmt.Errorf("create task memories dir: %w", err)
	}

	if err := reconcileHermesTaskHome(hermesHome); err != nil {
		return fmt.Errorf("reconcile hermes task home: %w", err)
	}
	if err := snapshotHermesRuntimeState(sharedHome, hermesHome); err != nil {
		return fmt.Errorf("snapshot hermes runtime state: %w", err)
	}
	if err := writeDerivedHermesConfig(sharedHome, hermesHome, logger); err != nil {
		return fmt.Errorf("derive hermes config: %w", err)
	}
	if err := writeDerivedHermesEnv(sharedHome, hermesHome); err != nil {
		return fmt.Errorf("derive hermes .env: %w", err)
	}
	return writeHermesBoundSkills(hermesHome, workspaceSkills, logger)
}

// writeDerivedHermesEnv writes the task-local .env: only explicitly allowlisted
// Hermes credentials/public settings from the source home's .env, followed by a
// pinned HERMES_HOME pointing at the overlay so it wins. Hermes loads
// <HERMES_HOME>/.env with override=True right after profile resolution, so
// without the pin an out-of-band HERMES_HOME= could relocate the home past the
// overlay. We always write the file — even when the source has none — so Hermes'
// project-.env fallback cannot supply owner/project state instead. Written 0600
// via atomic replace since it can hold API-key secrets.
func writeDerivedHermesEnv(sharedHome, hermesHome string) error {
	dst := filepath.Join(hermesHome, ".env")

	var body []byte
	src, err := readStableRegularFile(filepath.Join(sharedHome, ".env"), hermesRuntimeSnapshotMaxBytes, nil)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("read shared .env: %w", err)
		}
	} else {
		body = projectHermesDotenv(src)
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

// projectHermesDotenv keeps only assignments whose key is explicitly allowed
// for Hermes task execution. Comments, blanks, malformed lines, HERMES_HOME,
// and all unrelated owner variables are dropped rather than copied into task
// scratch. Assignment text is otherwise preserved so python-dotenv retains its
// native quoting and expansion behavior for allowed values.
func projectHermesDotenv(content []byte) []byte {
	lines := strings.Split(string(content), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if _, allowed := hermesOwnerEnvAllowlist[dotenvLineKey(line)]; !allowed {
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

// reconcileHermesTaskHome removes every top-level entry not owned by the task
// projection. This prevents unknown owner state or stale data from an older
// implementation surviving a Reuse.
func reconcileHermesTaskHome(hermesHome string) error {
	entries, err := os.ReadDir(hermesHome)
	if err != nil {
		return fmt.Errorf("read task home: %w", err)
	}
	for _, entry := range entries {
		if _, keep := hermesTaskHomeEntries[entry.Name()]; keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(hermesHome, entry.Name())); err != nil {
			return fmt.Errorf("remove unknown task-home entry %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func snapshotHermesRuntimeState(sharedHome, hermesHome string) error {
	for _, name := range []string{"auth.json", "oauth_state.json"} {
		if err := snapshotOptionalHermesFile(filepath.Join(sharedHome, name), filepath.Join(hermesHome, name)); err != nil {
			return fmt.Errorf("snapshot %s: %w", name, err)
		}
	}
	return snapshotHermesPlugins(sharedHome, hermesHome, nil)
}

func snapshotOptionalHermesFile(source, target string) error {
	if _, err := os.Lstat(source); os.IsNotExist(err) {
		return os.RemoveAll(target)
	} else if err != nil {
		return fmt.Errorf("inspect source: %w", err)
	}
	if info, err := os.Lstat(target); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("remove unsafe target: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect target: %w", err)
	}
	return snapshotRegularFile(source, target, hermesRuntimeSnapshotMaxBytes)
}

type hermesPluginSnapshotLimits struct {
	files int
	bytes int64
}

func snapshotHermesPlugins(sharedHome, hermesHome string, afterOpen func(string)) error {
	source := filepath.Join(sharedHome, "plugins")
	target := filepath.Join(hermesHome, "plugins")
	info, err := os.Lstat(source)
	if os.IsNotExist(err) {
		return os.RemoveAll(target)
	}
	if err != nil {
		return fmt.Errorf("inspect source: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: source is not a regular directory", errUnsafeHermesPluginEntry)
	}

	staging, err := os.MkdirTemp(hermesHome, ".hermes-plugins-snapshot-")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staging)
		}
	}()

	limits := hermesPluginSnapshotLimits{}
	if err := copyHermesPluginTree(source, staging, &limits, afterOpen); err != nil {
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove stale snapshot: %w", err)
	}
	if err := os.Rename(staging, target); err != nil {
		return fmt.Errorf("publish snapshot: %w", err)
	}
	published = true
	return nil
}

func copyHermesPluginTree(source, target string, limits *hermesPluginSnapshotLimits, afterOpen func(string)) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return fmt.Errorf("resolve plugin entry: %w", err)
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			if relative == "." {
				return nil
			}
			if err := os.Mkdir(destination, 0o700); err != nil {
				return fmt.Errorf("create plugin snapshot directory: %w", err)
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("%w: %s", errUnsafeHermesPluginEntry, relative)
		}
		limits.files++
		if limits.files > hermesPluginSnapshotMaxFiles {
			return fmt.Errorf("%w: %s", errHermesPluginLimit, relative)
		}
		var callback func()
		if afterOpen != nil {
			callback = func() { afterOpen(path) }
		}
		data, err := readStableRegularFile(path, hermesPluginSnapshotMaxFileBytes, callback)
		if err != nil {
			return fmt.Errorf("%w: %s: %v", errUnsafeHermesPluginEntry, relative, err)
		}
		if limits.bytes > hermesPluginSnapshotMaxBytes-int64(len(data)) {
			return fmt.Errorf("%w: %s", errHermesPluginLimit, relative)
		}
		limits.bytes += int64(len(data))
		if err := writePrivateSnapshot(destination, data, hermesPluginSnapshotMaxFileBytes); err != nil {
			return fmt.Errorf("write plugin snapshot %s: %w", relative, err)
		}
		return nil
	})
}

// writeDerivedHermesConfig writes a minimal task-local config.yaml. It projects
// only explicitly allowlisted public model-selection fields, sets an empty
// skills.external_dirs list, and disables external memory. Credentials, owner
// paths, unknown fields, and malformed
// source bytes never enter task scratch. The file is written 0600 via atomic
// replace so reuse also repairs prior permissions.
func writeDerivedHermesConfig(sharedHome, hermesHome string, logger *slog.Logger) error {
	srcConfig := filepath.Join(sharedHome, "config.yaml")
	dstConfig := filepath.Join(hermesHome, "config.yaml")
	derived := newHermesConfigDocument()
	var source *yaml.Node

	data, err := readStableRegularFile(srcConfig, hermesRuntimeSnapshotMaxBytes, nil)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("read shared config: %w", err)
		}
	} else {
		parsed := &yaml.Node{}
		if err := yaml.Unmarshal(data, parsed); err != nil {
			logger.Warn("execenv: hermes-home config parse failed; using safe minimal config", "error", err)
		} else {
			source = parsed
			projectHermesConfigFields(derived, source)
		}
	}
	if err := setHermesExternalDirs(derived, nil); err != nil {
		return err
	}
	// Disable any host-configured external memory backend (memory.provider) so a
	// Supermemory/Hindsight/etc. bank isn't shared across managed tasks; the
	// built-in per-task memories/ dir is already isolated above.
	disableHermesMemoryProvider(derived)
	return marshalYAMLToFile(derived, dstConfig)
}

func newHermesConfigDocument() *yaml.Node {
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
}

// projectHermesConfigFields preserves legacy scalar model/provider selection or
// reconstructs the modern model mapping from public allowlisted fields. Nodes
// are copied by value so the derived document does not retain pointers into the
// full owner config tree.
func projectHermesConfigFields(dst, source *yaml.Node) {
	dstTop := yamlDocumentRoot(dst)
	sourceTop := yamlDocumentRoot(source)
	if dstTop == nil || sourceTop == nil {
		return
	}

	model := yamlMapValue(sourceTop, "model")
	switch {
	case model == nil:
	case model.Kind == yaml.ScalarNode:
		yamlSetMapValue(dstTop, "model", cloneYAMLScalar(model))
	case model.Kind == yaml.MappingNode:
		projected := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, key := range hermesOwnerModelConfigAllowlist {
			if value := yamlMapValue(model, key); value != nil && value.Kind == yaml.ScalarNode {
				yamlSetMapValue(projected, key, cloneYAMLScalar(value))
			}
		}
		if entra := yamlMapValue(model, "entra"); entra != nil && entra.Kind == yaml.MappingNode {
			if scope := yamlMapValue(entra, "scope"); scope != nil && scope.Kind == yaml.ScalarNode {
				projectedEntra := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				yamlSetMapValue(projectedEntra, "scope", cloneYAMLScalar(scope))
				yamlSetMapValue(projected, "entra", projectedEntra)
			}
		}
		if len(projected.Content) > 0 {
			yamlSetMapValue(dstTop, "model", projected)
		}
	}

	if provider := yamlMapValue(sourceTop, "provider"); provider != nil && provider.Kind == yaml.ScalarNode {
		yamlSetMapValue(dstTop, "provider", cloneYAMLScalar(provider))
	}
}

func cloneYAMLScalar(source *yaml.Node) *yaml.Node {
	cloned := *source
	cloned.Content = nil
	return &cloned
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
		// Keep an explicit empty task-owned skills directory.
		return os.MkdirAll(skillsDir, 0o700)
	}
	// Skills live under env.RootDir/hermes-home, which the GC loop (cloud) or
	// env teardown (local_directory) wipes wholesale — no sidecar manifest.
	return writeSkillFiles(skillsDir, workspaceSkills, nil)
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

// marshalYAMLToFile renders a YAML node to dst as a 0600 file via atomic
// replace.
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
