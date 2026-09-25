package execenv

import (
	"path/filepath"
	"regexp"
	"strings"
)

// hookStateHeaderRe matches a `[hooks.state."<key>"]` table header. Codex keys
// hook trust by `<config source path>:<event>:<group>:<index>`, so the quoted
// key always starts with the absolute path of the file that declared the hook.
var hookStateHeaderRe = regexp.MustCompile(`^\s*\[\s*hooks\.state\."([^"\\]*)"\s*\]\s*(?:#.*)?$`)

// rehomeCodexHookTrust carries the user's hook trust decisions from the shared
// Codex home into a per-task codex-home.
//
// Background: Codex only runs a hook whose `[hooks.state."<key>"]` entry holds
// a trusted_hash matching the hook definition. The hash depends only on the
// definition, but the key embeds the absolute path of the declaring config
// file — `<CODEX_HOME>/config.toml:user_prompt_submit:0:0`. Codex looks the key
// up under CODEX_HOME exactly as given (symlinks unresolved), while its
// `hooks/list` reports the resolved path, so a trusting user may have either
// form. Multica copies ~/.codex/config.toml verbatim into the task's
// codex-home, so every inherited key still names the shared home and each
// already-trusted hook is silently skipped as untrusted.
//
// Every entry whose key starts with the shared home (raw or resolved) is
// rewritten to the task home, once per path form (raw and resolved), with its
// body — and so its trusted_hash — kept verbatim. Nothing new becomes trusted:
// only hashes the user already approved move, and a changed hook definition
// still mismatches its hash. Duplicate targets (both shared forms map to the
// same task keys) and keys already present in the file are emitted only once,
// since TOML rejects a table defined twice. All other lines are untouched.
func rehomeCodexHookTrust(content, sharedHome, taskHome string) string {
	if sharedHome == "" || taskHome == "" || !strings.Contains(content, "hooks.state.") {
		return content
	}
	sharedForms := codexHomePathForms(sharedHome)
	taskForms := codexHomePathForms(taskHome)

	lines := strings.Split(content, "\n")
	emitted := make(map[string]bool)
	for _, line := range lines {
		if m := hookStateHeaderRe.FindStringSubmatch(line); m != nil {
			if _, ok := trimCodexHomePrefix(m[1], sharedForms); !ok {
				emitted[m[1]] = true
			}
		}
	}

	out := make([]string, 0, len(lines))
	changed := false
	for i := 0; i < len(lines); {
		m := hookStateHeaderRe.FindStringSubmatch(lines[i])
		rest, ok := "", false
		if m != nil {
			rest, ok = trimCodexHomePrefix(m[1], sharedForms)
		}
		if !ok {
			out = append(out, lines[i])
			i++
			continue
		}

		// The table body runs to the next header. Its trailing blank/comment
		// lines belong to whatever follows (e.g. a managed-block END marker),
		// so they are emitted once after the rewritten tables.
		end := i + 1
		for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "[") {
			end++
		}
		bodyEnd := end
		for bodyEnd > i+1 {
			t := strings.TrimSpace(lines[bodyEnd-1])
			if t != "" && !strings.HasPrefix(t, "#") {
				break
			}
			bodyEnd--
		}
		body := lines[i+1 : bodyEnd]

		for _, form := range taskForms {
			key := form + rest
			if emitted[key] {
				continue
			}
			emitted[key] = true
			out = append(out, strings.Replace(lines[i], `"`+m[1]+`"`, `"`+key+`"`, 1))
			out = append(out, body...)
		}
		out = append(out, lines[bodyEnd:end]...)
		changed = true
		i = end
	}
	if !changed {
		return content
	}
	return strings.Join(out, "\n")
}

// codexHomePathForms returns the path as given plus, when it differs, its
// symlink-resolved form.
func codexHomePathForms(p string) []string {
	forms := []string{p}
	if resolved, err := filepath.EvalSymlinks(p); err == nil && resolved != p {
		forms = append(forms, resolved)
	}
	return forms
}

// trimCodexHomePrefix reports whether key names a file directly inside one of
// the home path forms and returns the remainder, including the separator.
func trimCodexHomePrefix(key string, forms []string) (string, bool) {
	for _, form := range forms {
		for _, sep := range []string{"/", string(filepath.Separator)} {
			if strings.HasPrefix(key, form+sep) {
				return key[len(form):], true
			}
		}
	}
	return "", false
}
