// Package sandbox generates macOS Seatbelt (sandbox-exec) profiles used to
// confine spawned coding-agent processes.
//
// The profile is deny-by-default: only explicit allow rules expose system
// directories, and writes are limited to the task work directory plus a
// per-task temp directory. Outbound network access is restricted to a
// port-level allowlist derived from host:port pairs supplied by the daemon
// (Multica server, agent provider API, loopback for the daemon health port,
// plus user-configured extras).
//
// Seatbelt limitation: the (remote tcp ...) primitive only accepts "*" or
// "localhost" as the host token. Per-hostname filtering at the kernel layer
// is therefore not possible — we degrade to per-port filtering, with
// loopback specially scoped so the daemon health port is reachable only on
// localhost. Hostname-level enforcement is an application-level concern
// (Claude Code hooks, outbound proxy) and is intentionally out of scope here.
//
// The profile shape was chosen so that the kernel-level rules survive even
// if Multica's application-level permission policy (Claude Code hooks) has
// a bug or is bypassed: the seatbelt is the failsafe.
package sandbox

// CEREBRO-PATCH(agent-sandbox-macos): cerebro modification of upstream file

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Profile describes the inputs needed to generate a Seatbelt profile.
type Profile struct {
	// Workdir is the task working directory. file-read* and file-write* are
	// allowed under this subpath.
	Workdir string
	// Home is the user's home directory. Reads under $HOME are broadly
	// allowed; sensitive subpaths (~/.ssh, ~/.aws, ~/.gcloud,
	// ~/Library/Application Support, ~/Library/Cookies, …) are denied
	// after the broad allow so they remain sealed even if a future allow
	// rule overlaps.
	Home string
	// TempDir is the task-specific temp directory. file-read*/file-write*
	// are allowed under this subpath. Optional — if empty, only Workdir
	// receives write access.
	TempDir string
	// WritablePaths are absolute paths the spawned process needs read+write
	// access to in addition to Workdir/TempDir. The agent CLIs (claude,
	// cursor, gemini, …) keep per-user state outside the workdir
	// (~/.claude.json, ~/.cursor, ~/.gemini, …) and silently fail to start
	// if those paths are denied. Callers (the daemon) populate this list
	// per provider; the generator emits the allow rules before the
	// sensitive-home denies so a path that accidentally overlaps a
	// denied subpath still gets sealed.
	WritablePaths []string
	// AllowedHosts is the outbound network allowlist as host:port pairs,
	// e.g. "api.anthropic.com:443" or "127.0.0.1:19514". Empty list means
	// outbound network access is fully denied.
	//
	// Seatbelt cannot match arbitrary hostnames or IP literals at the
	// kernel layer (only "*" and "localhost"), so the generator translates
	// this list into port-level rules: loopback hosts produce a
	// "localhost:<port>" rule; all other hosts collapse to a "*:<port>"
	// rule. Application-level hostname filtering (e.g. Claude Code hooks,
	// outbound proxy) is required if hostname-level enforcement is needed.
	AllowedHosts []string
}

// Generate renders the Seatbelt profile as a Scheme-DSL string.
//
// Rule ordering is significant: sandbox-exec evaluates rules top-to-bottom
// and the LAST matching rule wins. The denies for sensitive HOME subpaths
// therefore appear after any allows, so a future broad allow on $HOME
// cannot accidentally re-expose them.
func Generate(p Profile) (string, error) {
	if p.Workdir == "" {
		return "", fmt.Errorf("sandbox: workdir is required")
	}
	if p.Home == "" {
		return "", fmt.Errorf("sandbox: home is required")
	}
	// sandbox-exec matches rules against the kernel-canonical path. We must
	// resolve symlinks (e.g. /var → /private/var on macOS) so that a rule
	// for "/var/folders/foo" is not bypassed by the kernel rewriting the
	// access to "/private/var/folders/foo" before checking rules.
	workdir, err := canonicalPath(p.Workdir)
	if err != nil {
		return "", fmt.Errorf("sandbox: resolve workdir: %w", err)
	}
	home, err := canonicalPath(p.Home)
	if err != nil {
		return "", fmt.Errorf("sandbox: resolve home: %w", err)
	}

	var b strings.Builder
	b.WriteString(";; Multica agent sandbox — generated, do not edit manually.\n")
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	b.WriteString("(debug deny)\n")
	b.WriteString("\n")

	// Process control. Without process-fork/exec the agent CLI cannot spawn
	// the tools it needs (git, language compilers, the Multica CLI, ...).
	b.WriteString(";; Process & signal\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow signal (target self))\n")
	b.WriteString("\n")

	// IPC and system info lookups. Without these, common macOS APIs
	// (resolv, dyld, locale lookups, ...) fail.
	b.WriteString(";; System lookups & IPC\n")
	b.WriteString("(allow mach-lookup)\n")
	b.WriteString("(allow ipc-posix-shm)\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow iokit-open)\n")
	b.WriteString("(allow file-ioctl)\n")
	b.WriteString("\n")

	// Read-only access to system locations. file-read-metadata is allowed
	// everywhere so stat() does not fail across the filesystem. The root
	// inode is allowed as a literal so path traversal of "/" works — without
	// it, dyld cannot resolve absolute paths and tools abort during startup.
	b.WriteString(";; Filesystem reads (system)\n")
	b.WriteString("(allow file-read-metadata)\n")
	b.WriteString("(allow file-read* (literal \"/\"))\n")
	for _, p := range systemReadPaths {
		fmt.Fprintf(&b, "(allow file-read* (subpath %s))\n", schemeString(p))
	}
	b.WriteString("\n")

	// Workdir + per-task temp directory: full read/write.
	b.WriteString(";; Workdir & temp (read+write)\n")
	fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", schemeString(workdir))
	if p.TempDir != "" {
		td, err := canonicalPath(p.TempDir)
		if err != nil {
			return "", fmt.Errorf("sandbox: resolve temp dir: %w", err)
		}
		fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", schemeString(td))
	}
	// /private/var/folders is the canonical Apple temp dir resolver location;
	// many CLIs create temp files there via os.TempDir().
	b.WriteString("(allow file-read* file-write* (subpath \"/private/var/folders\"))\n")
	b.WriteString("(allow file-read* file-write* (subpath \"/private/tmp\"))\n")
	b.WriteString("(allow file-read* file-write* (subpath \"/tmp\"))\n")
	b.WriteString("\n")

	// Devices that any normal process expects to read/write.
	b.WriteString(";; Standard devices\n")
	b.WriteString("(allow file-read* file-write* (literal \"/dev/null\") (literal \"/dev/zero\")\n")
	b.WriteString("    (literal \"/dev/random\") (literal \"/dev/urandom\")\n")
	b.WriteString("    (literal \"/dev/dtracehelper\") (literal \"/dev/tty\"))\n")
	b.WriteString("(allow file-read* (subpath \"/dev/fd\"))\n")
	b.WriteString("(allow file-read* (subpath \"/dev/null\"))\n")
	b.WriteString("\n")

	// Home: broad read access. The previous design enumerated 16 specific
	// subpaths under $HOME (~/.cache, ~/.npm, ~/Library/Caches, …) which
	// missed every agent-CLI state path (~/.claude.json, ~/.cursor,
	// ~/.gemini, …) and caused claude to exit 0 with empty output the
	// instant it tried to read its own settings.json (JEH-405). The
	// security model has not weakened: secrets are protected by the
	// explicit deny list emitted below, which runs LAST and overrides
	// any preceding allow under "last match wins". Auditing what is
	// sealed is now driven by the deny list (which we control fully)
	// rather than by an opt-in allow list (which has to anticipate
	// every CLI's storage choices).
	b.WriteString(";; Home (broad read; sensitive subpaths denied below)\n")
	fmt.Fprintf(&b, "(allow file-read* (subpath %s))\n", schemeString(home))
	b.WriteString("\n")

	// Per-process writable paths outside the workdir (agent CLI state
	// files like ~/.claude.json, ~/.cursor, ~/.gemini). These are emitted
	// BEFORE the sensitive-home deny list so an accidental overlap with a
	// denied path still gets sealed by the trailing deny.
	if len(p.WritablePaths) > 0 {
		b.WriteString(";; Per-process writable paths (agent CLI state)\n")
		for _, raw := range p.WritablePaths {
			resolved, err := canonicalPath(raw)
			if err != nil {
				return "", fmt.Errorf("sandbox: resolve writable path %q: %w", raw, err)
			}
			fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", schemeString(resolved))
		}
		b.WriteString("\n")
	}

	b.WriteString(";; Sensitive home subpaths — DENY (defense in depth)\n")
	for _, sub := range sensitiveHomeSubpaths {
		fmt.Fprintf(&b, "(deny file-read* file-write* (subpath %s))\n", schemeString(filepath.Join(home, sub)))
	}
	// Keychain DB: read is required so securityd can serve the agent CLI's
	// own item via XPC; the actual cross-item authorisation is enforced by
	// per-item ACLs inside securityd (a sandboxed claude can only decrypt
	// items the user's keychain ACL grants to the claude binary). Writes
	// would only be a DOS / corruption vector — securityd is the legitimate
	// path for mutations and runs unsandboxed.
	for _, sub := range keychainReadOnlySubpaths {
		fmt.Fprintf(&b, "(deny file-write* (subpath %s))\n", schemeString(filepath.Join(home, sub)))
	}
	b.WriteString("\n")

	// Outbound network. Seatbelt's (remote tcp ...) only accepts "*" or
	// "localhost" as the host token, so we translate the host:port
	// allowlist into kernel-valid rules: loopback hosts become
	// "localhost:<port>", all other hosts collapse to "*:<port>". This
	// loses hostname-level filtering at the kernel layer (a Seatbelt
	// limitation, not a design choice) but keeps the loopback scope
	// distinct so non-loopback callers cannot reach the daemon health
	// port.
	b.WriteString(";; Network (deny by default; allow specific ports)\n")
	b.WriteString(";; Seatbelt cannot match hostnames; rules are port-scoped.\n")
	rules := translateAllowlistToTCPRules(p.AllowedHosts)
	if len(rules) == 0 {
		b.WriteString(";; (allowlist is empty — all outbound TCP is denied)\n")
	} else {
		for _, r := range rules {
			b.WriteString(r)
			b.WriteByte('\n')
		}
	}
	// DNS is needed to resolve allowlisted hostnames. Without this, even
	// allowed hosts cannot be reached.
	//
	// On macOS, getaddrinfo does NOT send raw UDP/53 packets — it talks to
	// mDNSResponder over a unix-domain socket at /private/var/run/mDNSResponder.
	// Missing this rule causes "Could not resolve host" inside the sandbox
	// even with the UDP/53 allow in place; the *:53 rule remains for the
	// rare static-config path that does issue UDP queries directly.
	b.WriteString("(allow network-outbound (literal \"/private/var/run/mDNSResponder\"))\n")
	b.WriteString("(allow network-outbound (remote udp \"*:53\"))\n")
	b.WriteString("(allow network-bind (local ip \"localhost:*\"))\n")
	b.WriteString("\n")

	return b.String(), nil
}

// WriteToTemp generates a profile and writes it to a temporary file. The
// caller is responsible for removing the file when the spawned process has
// exited (typically via the cleanup function returned by ApplyToCommand).
func WriteToTemp(p Profile) (string, error) {
	contents, err := Generate(p)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "multica-sandbox-*.sb")
	if err != nil {
		return "", fmt.Errorf("sandbox: create temp file: %w", err)
	}
	if _, err := f.WriteString(contents); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("sandbox: write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("sandbox: close temp file: %w", err)
	}
	return f.Name(), nil
}

// systemReadPaths are subpaths the agent CLI legitimately needs to read so
// dynamic linking, locales, framework loading, and Homebrew-installed tools
// resolve correctly.
var systemReadPaths = []string{
	"/usr",
	"/bin",
	"/sbin",
	"/System",
	"/Library",
	"/opt/homebrew",
	"/opt/local",
	"/private/etc",
	"/private/var/db",
	"/private/var/select",
	"/Applications",
}

// sensitiveHomeSubpaths are denied unconditionally. Emitted last so they
// override the broad $HOME read allow and any per-process writable-path
// allow that happens to overlap (last match wins).
var sensitiveHomeSubpaths = []string{
	".ssh",
	".aws",
	".gcloud",
	".config/gcloud",
	".azure",
	".kube",
	".docker",
	".gnupg",
	".password-store",
	".netrc",
	"Library/Application Support",
	"Library/Cookies",
	"Library/Mail",
	"Library/Messages",
}

// keychainReadOnlySubpaths are home subpaths that need file-read so the
// agent CLI can fetch its own auth token via securityd, but where direct
// file-write is denied. securityd enforces per-item ACLs at decrypt time,
// so reading the keychain DB does not grant access to other apps' items.
// Mutations to the keychain go through securityd (unsandboxed) anyway, so
// blocking file-write here only stops corruption / cooperative-mmap
// shenanigans, never legitimate use.
var keychainReadOnlySubpaths = []string{
	"Library/Keychains",
}

// translateAllowlistToTCPRules converts host:port pairs into the
// (allow network-outbound ...) rules that macOS Seatbelt actually accepts.
//
// Why a translation step exists: sandbox-exec parses the .sb profile up
// front and refuses to launch the wrapped process if any rule is invalid —
// it exits 65 before the agent ever runs. The (remote tcp ...) primitive
// only accepts "*" or "localhost" as the host token; an IP literal or FQDN
// produces "host must be * or localhost in network address" and aborts.
// Per-host filtering at the kernel layer is therefore impossible on macOS,
// regardless of how the allowlist is configured.
//
// Translation rules:
//   - Loopback hosts (localhost, 127.0.0.1, ::1, anything else
//     net.IP.IsLoopback) → "localhost:<port>".
//   - All other hosts (FQDNs, public IPs) → "*:<port>" (port-only filter).
//
// Returns deduplicated, deterministically-ordered (allow ...) lines. An
// empty input slice returns nil so callers can emit a clear "denied"
// comment instead.
func translateAllowlistToTCPRules(in []string) []string {
	loopbackPorts := map[string]struct{}{}
	publicPorts := map[string]struct{}{}
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		host, port, err := net.SplitHostPort(raw)
		if err != nil || host == "" || port == "" {
			continue
		}
		if isLoopbackHost(host) {
			loopbackPorts[port] = struct{}{}
		} else {
			publicPorts[port] = struct{}{}
		}
	}
	if len(loopbackPorts)+len(publicPorts) == 0 {
		return nil
	}
	out := make([]string, 0, len(loopbackPorts)+len(publicPorts))
	for _, p := range sortedKeys(loopbackPorts) {
		out = append(out, fmt.Sprintf("(allow network-outbound (remote tcp %s))", schemeString("localhost:"+p)))
	}
	for _, p := range sortedKeys(publicPorts) {
		out = append(out, fmt.Sprintf("(allow network-outbound (remote tcp %s))", schemeString("*:"+p)))
	}
	return out
}

// isLoopbackHost reports whether a host token from a host:port pair refers
// to the loopback interface. We accept the literal "localhost", IPv4/IPv6
// loopback literals, and anything else net.IP.IsLoopback recognises.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// sortedKeys returns the keys of a string-keyed set in lexicographic order,
// so generated profiles are deterministic regardless of map iteration.
func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// normalizeHosts returns a deduplicated, sorted list of host:port entries.
// Entries that cannot be parsed are silently dropped (the daemon is the
// trusted source; the only realistic failure is a typo in the env var).
func normalizeHosts(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		host, port, err := net.SplitHostPort(raw)
		if err != nil || host == "" || port == "" {
			continue
		}
		// SplitHostPort strips brackets from IPv6 — re-add them so the
		// sandbox-exec parser accepts the literal.
		key := raw
		if strings.Contains(host, ":") {
			key = "[" + host + "]:" + port
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// canonicalPath resolves symlinks and returns an absolute path. On macOS,
// directories like /var, /tmp, and /etc are symlinks to /private/var,
// /private/tmp, /private/etc — and sandbox-exec applies rules against the
// canonical path. If EvalSymlinks fails (e.g. a leaf component does not
// exist yet), fall back to the lexically-resolved absolute path so the
// caller still gets a usable rule.
func canonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs, nil //nolint:nilerr // fall back to lexical absolute path
	}
	return resolved, nil
}

// schemeString quotes a Go string so it can be embedded as a Scheme literal
// in the .sb profile. The Seatbelt parser accepts standard double-quoted
// strings with backslash escapes for `"` and `\`.
func schemeString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
