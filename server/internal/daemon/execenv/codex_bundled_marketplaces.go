package execenv

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"

	"github.com/multica-ai/multica/server/internal/util"
)

// codexBundledMarketplacesRelPath is the directory under a CODEX_HOME where
// the Codex CLI resolves bundled plugin marketplaces (openai-bundled:
// browser, chrome, computer-use, ...). The same relative location inside the
// shared home is where ChatGPT Desktop materializes them from the app bundle.
const codexBundledMarketplacesRelPath = ".tmp" + string(filepath.Separator) + "bundled-marketplaces"

// exposeSharedCodexBundledMarketplaces links the shared home's bundled
// marketplaces into the per-task CODEX_HOME and repoints the task config's
// local marketplace sources at the linked copy.
//
// Why this exists: the user's shared ~/.codex/config.toml registers the
// bundled plugin marketplaces with an absolute local source inside the shared
// home, because ChatGPT Desktop materializes them there (for example
// `~/.codex/.tmp/bundled-marketplaces/openai-bundled`). The Codex CLI resolves
// bundled-marketplace sources against the ACTIVE CODEX_HOME and silently
// drops any whose source is not inside it — a config copied verbatim into the
// per-task home therefore loses the whole openai-bundled marketplace, even
// though the shared directory exists and is readable.
//
// The browser plugin in that marketplace owns the Stop / Interrupt /
// SubagentStop hooks that notify the node_repl MCP server (`turn_ended`) when
// a turn finishes. Without the marketplace the hooks never load, and a task
// that drove the browser through node_repl leaves its tabs behind: unmarked
// tabs stay open after completion, and a markDeliverable tab keeps the
// finished session's browser-session lock so a later session's claimTab fails.
//
// The link mirrors exposeSharedCodexPluginCache: one directory link, shared
// contents stay shared, task reclamation only ever removes the link. A shared
// home without bundled marketplaces (a CLI-only machine) is a no-op — the
// daemon never creates the shared directory, it only surfaces what the
// desktop app already materialized.
func exposeSharedCodexBundledMarketplaces(codexHome, sharedHome string) error {
	src := filepath.Join(sharedHome, filepath.FromSlash(codexBundledMarketplacesRelPath))
	fi, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			// Nothing materialized in the shared home — nothing to expose. Do
			// not create the shared directory: bundled marketplaces belong to
			// the desktop app, not to the daemon.
			return nil
		}
		// A read failure is not "absent": swallowing it would leave the task
		// silently without its marketplaces and the operator without a clue
		// (the caller warns on every error this function returns).
		return fmt.Errorf("stat shared bundled marketplaces %s: %w", src, err)
	}
	if !fi.IsDir() {
		return nil
	}
	dst := filepath.Join(codexHome, filepath.FromSlash(codexBundledMarketplacesRelPath))
	if err := verifyTaskHomeComponentsForLink(codexHome, filepath.Dir(dst)); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create codex .tmp dir: %w", err)
	}

	if fi, err := os.Lstat(dst); err == nil {
		isLink := fi.Mode()&os.ModeSymlink != 0
		if isLink {
			if target, readlinkErr := os.Readlink(dst); readlinkErr == nil && target == src {
				return rewriteSharedBundledMarketplaceSources(filepath.Join(codexHome, "config.toml"), src, dst)
			}
			if err := os.Remove(dst); err != nil {
				return fmt.Errorf("remove stale bundled marketplaces link: %w", err)
			}
		} else {
			if err := os.RemoveAll(dst); err != nil {
				return fmt.Errorf("remove stale bundled marketplaces path: %w", err)
			}
		}
	}

	if err := createDirLink(src, dst); err != nil {
		return fmt.Errorf("expose shared bundled marketplaces: %w", err)
	}
	return rewriteSharedBundledMarketplaceSources(filepath.Join(codexHome, "config.toml"), src, dst)
}

// verifyTaskHomeComponentsForLink refuses to touch the link destination when
// a path component the daemon is about to create into, delete through, or
// place a link under is itself a link out of the task home. Reuse hands the
// daemon a home a previous task (and its descendants, unconfirmed on
// Windows) could write; if `.tmp` had been swapped for a symlink, MkdirAll
// and RemoveAll here would follow it outside the home. The leaf itself is
// safe either way — os.Remove/RemoveAll only ever unlink a symlinked leaf —
// so the check is about the interior, not the destination.
func verifyTaskHomeComponentsForLink(codexHome, tmpDir string) error {
	if fi, err := os.Lstat(codexHome); err != nil {
		return fmt.Errorf("stat codex home %s: %w", codexHome, err)
	} else if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return fmt.Errorf("codex home %s is not a real directory; refusing to place bundled marketplaces link", codexHome)
	}
	fi, err := os.Lstat(tmpDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat codex home .tmp %s: %w", tmpDir, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return fmt.Errorf("codex home .tmp %s is not a real directory; refusing to place bundled marketplaces link", tmpDir)
	}
	return nil
}

// rewriteSharedBundledMarketplaceSources rewrites the per-task config.toml's
// local marketplace sources that point into the shared home's bundled
// marketplaces directory so they point at the same location inside the task
// home instead.
//
// The rewrite works on the TOML AST, not on lines: go-toml's unstable parser
// reports every key/value's exact byte range, so whichever of the many valid
// spellings a config uses — plain tables, trailing comments, bare or quoted
// keys, dotted keys at the top level, inline tables, single- or multi-line
// strings — the same logical field is found and nothing else can be touched.
// That last part is the safety property: a string VALUE that happens to
// contain text like `source = "..."` (an MCP env entry, say) is one AST node
// whose bytes are replaced only if its key path is a marketplace source, and
// its interior is never scanned. A config the parser rejects is left
// untouched (and reported): the CLI would reject it too, so there is nothing
// for this step to fix.
//
// A source is repointed only when it resolves inside sharedBundledRoot,
// which is precise enough on its own: Git marketplace sources are URLs, and
// other local marketplaces (e.g. openai-primary-runtime under ~/.cache)
// resolve elsewhere and keep their absolute paths — the CLI only requires
// bundled ones to live inside the active CODEX_HOME. A source already spelled
// as its task-home destination maps back to itself through the link and is
// reported as "nothing to change", so prepare and reuse can both run it
// without ever rewriting the same bytes twice.
func rewriteSharedBundledMarketplaceSources(configPath, sharedBundledRoot, codexBundledRoot string) error {
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read config.toml: %w", err)
	}

	replacements, err := locateMarketplaceSourceRewrites(data, sharedBundledRoot, codexBundledRoot)
	if err != nil {
		return fmt.Errorf("parse config.toml marketplaces: %w", err)
	}
	if len(replacements) == 0 {
		return nil
	}

	out := data
	// Apply back-to-front so the offsets of the untouched replacements stay
	// valid while earlier ones splice the buffer down.
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		spliced := make([]byte, 0, len(out)-(r.end-r.start)+len(r.quoted))
		spliced = append(spliced, out[:r.start]...)
		spliced = append(spliced, r.quoted...)
		spliced = append(spliced, out[r.end:]...)
		out = spliced
	}
	if bytes.Equal(out, data) {
		return nil
	}
	if err := os.WriteFile(configPath, out, 0o644); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	return nil
}

// sourceReplacement replaces the byte range [start,end) of a config (a
// marketplace source string literal) with the quoted text of the new path.
type sourceReplacement struct {
	start, end int
	quoted     string
}

// locateMarketplaceSourceRewrites parses config content and returns the byte
// ranges of every marketplace source value that must move from
// sharedBundledRoot into codexBundledRoot. A parse failure returns an error
// rather than a guess: the daemon must not rewrite a config it cannot read.
func locateMarketplaceSourceRewrites(data []byte, sharedBundledRoot, codexBundledRoot string) ([]sourceReplacement, error) {
	var parser unstable.Parser
	parser.Reset(data)

	var currentPath []string
	var replacements []sourceReplacement
	for parser.NextExpression() {
		expr := parser.Expression()
		switch expr.Kind {
		case unstable.Table, unstable.ArrayTable:
			currentPath = unstableKeyPath(expr)
		case unstable.KeyValue:
			value := expr.Child()
			keys := unstableKeyPath(expr)
			path := append(append([]string{}, currentPath...), keys...)
			walkMarketplaceValues(value, path, sharedBundledRoot, codexBundledRoot, &replacements)
		}
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	return replacements, nil
}

// unstableKeyPath decodes the dotted key of a Table / ArrayTable node, or the
// key part of a KeyValue node (its children after the first), into logical
// segments with quoting already resolved by the parser.
func unstableKeyPath(keyOwner *unstable.Node) []string {
	var path []string
	for it := keyOwner.Key(); it.Next(); {
		path = append(path, string(it.Node().Data))
	}
	return path
}

// walkMarketplaceValues descends a key/value's value node collecting
// replacements for string values whose key path names a marketplace source
// (`marketplaces.<name>.source`, including deeper sub-tables and inline or
// dotted spellings). Arrays and inline tables are recursed into so every
// container form of the same logical field is covered; scalar non-strings
// cannot be a marketplace source and are skipped.
func walkMarketplaceValues(value *unstable.Node, path []string, sharedBundledRoot, codexBundledRoot string, replacements *[]sourceReplacement) {
	switch value.Kind {
	case unstable.String:
		if isMarketplaceSourcePath(path) {
			recordMarketplaceSource(value, string(value.Data), sharedBundledRoot, codexBundledRoot, replacements)
		}
	case unstable.InlineTable:
		for child := value.Child(); child.Valid(); child = child.Next() {
			if child.Kind != unstable.KeyValue {
				continue // comments and other meta nodes
			}
			childValue := child.Child()
			childPath := append(append([]string{}, path...), unstableKeyPath(child)...)
			walkMarketplaceValues(childValue, childPath, sharedBundledRoot, codexBundledRoot, replacements)
		}
	case unstable.Array:
		for element := value.Child(); element.Valid(); element = element.Next() {
			walkMarketplaceValues(element, path, sharedBundledRoot, codexBundledRoot, replacements)
		}
	}
}

// isMarketplaceSourcePath reports whether a decoded key path names the source
// of one marketplace inside the [marketplaces] collection: exactly
// `marketplaces.<name>.source`. That three-segment shape is the marketplace
// definition field the CLI reads; anything deeper nested under a marketplace
// (metadata sub-tables and the like) is unrelated content that merely shares
// the root and must keep its own value, and a bare `marketplaces.source` is
// not a marketplace entry at all. Both are left alone.
func isMarketplaceSourcePath(path []string) bool {
	return len(path) == 3 && path[0] == "marketplaces" && path[2] == "source"
}

// recordMarketplaceSource appends a replacement for one source value when it
// resolves inside the shared bundled root. Surrounding whitespace the value
// spelling carried (a multi-line literal's leading/trailing newlines, say) is
// trimmed before the path checks — the CLI trims it too — and the replacement
// is always the canonical quoted form of the relocated path.
func recordMarketplaceSource(value *unstable.Node, src string, sharedBundledRoot, codexBundledRoot string, replacements *[]sourceReplacement) {
	relocated, ok := repointSharedBundledSource(strings.TrimSpace(src), sharedBundledRoot, codexBundledRoot)
	if !ok {
		return
	}
	*replacements = append(*replacements, sourceReplacement{
		start:  int(value.Raw.Offset),
		end:    int(value.Raw.Offset + value.Raw.Length),
		quoted: strconv.Quote(relocated),
	})
}

// repointSharedBundledSource maps src into codexBundledRoot when src resolves
// inside sharedBundledRoot, preserving the relative layout. It reports false
// for anything else: a source that resolves outside the shared bundled root
// (URLs, other local marketplaces, sources already inside the task home) and
// a shared source that no longer exists.
//
// A source already spelled as a codexBundledRoot path decodes — through the
// link — back into sharedBundledRoot and would map to itself; that mapping is
// detected and reported as "nothing to change", so prepare and reuse re-runs
// do not rewrite the config with the same bytes over and over. The comparison
// happens on the mapped result, not the input spelling, so an unusual but
// equivalent spelling of the task-home path still converges to the canonical
// one exactly once.
//
// Both roots are resolved the way the OS opens them (util.ResolveSymlinks,
// which on Windows follows directory junctions that filepath.EvalSymlinks
// refuses to traverse) so a shared home reached via a link — the $CODEX_HOME
// fallback in resolveSharedCodexHome, or a home moved across drives on
// Windows — still matches the literal paths the copied config carries.
func repointSharedBundledSource(src, sharedBundledRoot, codexBundledRoot string) (string, bool) {
	if !filepath.IsAbs(src) {
		return "", false
	}
	evalRoot, err := util.ResolveSymlinks(sharedBundledRoot)
	if err != nil {
		return "", false
	}
	evalSrc, err := util.ResolveSymlinks(src)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(evalRoot, evalSrc)
	if err != nil || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	relocated := filepath.Join(codexBundledRoot, rel)
	if relocated == src {
		// Already pointed at the task-home copy (idempotent re-prepare).
		return "", false
	}
	return relocated, true
}
