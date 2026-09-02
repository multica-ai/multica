package wecom

// progress_secrets.go — keeping credentials out of the progress bubble.
//
// The bubble shows what the agent is doing, and what it is doing is often a
// command line: `bash -lc 'curl -H "Authorization: Bearer sk-…" …'`, an MCP
// call carrying api_token=…, a clone URL with a password in it. Rendered
// verbatim, that secret is in the chat.
//
// What makes it worth defending rather than shrugging at: WeCom has no edit
// and no unsend (see the note at the top of stream_store.go), and the message
// is retained by the tenant's own archive. A credential that reaches a bubble
// once is in a corporate record permanently, readable by anyone with archive
// access, and the only remedy left is rotation.
//
// The mechanism follows the one this package already argued for in
// progress_render.go: a list of key names is the cheap first pass, and the
// SHAPE of the value is the real test. A denylist only knows what somebody
// already wrote down — the next provider spells it `code`, or `passphrase` —
// so it cannot be the thing carrying the weight.
//
// Redaction, not suppression. "正在运行 curl -H Authorization: ███ https://…"
// still tells the reader what the agent is doing, which is the whole point of
// the bubble; dropping the line would trade one failure for another.

import (
	"regexp"
	"strings"
	"unicode"
)

// secretMask is what replaces a secret. Distinctive enough that a reader
// recognises the redaction rather than reading it as part of the command.
const secretMask = "███"

// Known credential prefixes. These are worth naming individually because they
// are unambiguous: a token beginning `sk-` or `ghp_` or `xoxb-` is a
// credential and nothing else, whatever key it arrived under and however
// short it is.
var secretPrefixes = []string{
	"sk-",      // OpenAI and everything that copied it
	"sk_live_", // Stripe
	"sk_test_", //
	"rk_live_", //
	"ghp_",     // GitHub personal access
	"gho_",     // GitHub OAuth
	"ghs_",     // GitHub server-to-server
	"ghu_",     // GitHub user-to-server
	"ghr_",     // GitHub refresh
	"github_pat_",
	"xoxb-", // Slack bot
	"xoxp-", // Slack user
	"xoxa-", //
	"xapp-", // Slack app-level
	"AKIA",  // AWS access key id
	"ASIA",  // AWS session key id
	"AIza",  // Google API key
	"ya29.", // Google OAuth access token
	"hf_",   // Hugging Face
	"glpat-",
	"npm_",
	"dop_v1_", // DigitalOcean
	"shpat_",  // Shopify
	"eyJ",     // a JWT's base64 header, `{"` — three of them make a token
}

// assignmentPattern finds `key=value` / `key: value` / `"key": "value"` where
// the key names something secret. This is the cheap pass: it catches a short
// or unremarkable value that shape alone would never flag, e.g.
// `password=hunter2`.
//
// The separator group tolerates the quote a JSON body puts between the key and
// the colon, because a tool's arguments arrive as JSON as often as a shell line. The value stops at whitespace or a quote, so a redaction cannot
// swallow the rest of the command.
var assignmentPattern = regexp.MustCompile(
	`(?i)(^|[^A-Za-z0-9])([A-Za-z0-9]+[_-])*` +
		`(pass(?:word|wd|phrase)?|secret[_-]?key|secret|(?:auth|access|refresh|bearer|id)[_-]?token|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|credential|authorization|session[_-]?id|client[_-]?secret)` +
		`(["']?\s*[:=]\s*["']?)` +
		`([^\s,;&"']+)`)

// bearerPattern catches the header form, where the key is the scheme rather
// than a parameter name: `Authorization: Bearer <token>`, `-H "token: <x>"`.
var bearerPattern = regexp.MustCompile(`(?i)\b(bearer|basic|token)(\s+)([A-Za-z0-9._\-+/=]{8,})`)

// schemeOnlyValue is the set an assignment must not redact: they name the
// scheme rather than carry the secret, and bearerPattern has already dealt
// with whatever followed them.
var schemeOnlyValue = map[string]struct{}{"bearer": {}, "basic": {}, "digest": {}, "negotiate": {}}

// urlCredentialPattern catches `scheme://user:password@host` — the shape a
// clone URL or a database DSN takes when someone inlined the password.
var urlCredentialPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)([^\s/:@]+):([^\s/@]+)@`)

// basicAuthPattern catches `curl -u user:password`, which carries the secret
// in an argument with no key name at all.
var basicAuthPattern = regexp.MustCompile(`(-u\s+)([^\s:]+):([^\s]+)`)

// flagPattern catches a long-flag credential: `--token abc`, `--password=abc`.
var flagPattern = regexp.MustCompile(
	`(?i)(--(?:pass(?:word|wd|phrase)?|secret|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|credential)[= ])` +
		`("[^"]*"|'[^']*'|[^\s,;&"']+)`)

// redactSecrets rewrites every credential it can recognise in s.
//
// Order matters only in that the URL form must run before the assignment form,
// or `://user:pass@` gets partially rewritten by the `pass`-adjacent rules and
// the result is neither redacted nor readable.
func redactSecrets(s string) string {
	if s == "" {
		return s
	}
	s = urlCredentialPattern.ReplaceAllString(s, "${1}${2}:"+secretMask+"@")
	s = flagPattern.ReplaceAllString(s, "${1}"+secretMask)
	s = basicAuthPattern.ReplaceAllString(s, "${1}${2}:"+secretMask)
	// bearerPattern first: "Authorization: Bearer <token>" is an assignment
	// whose value is the SCHEME, not the secret. Letting the assignment rule
	// have it turns a readable header into "Authorization: ███ ███" and hides
	// which scheme was in use — the one piece of that line worth keeping.
	s = bearerPattern.ReplaceAllString(s, "${1}${2}"+secretMask)
	s = assignmentPattern.ReplaceAllStringFunc(s, func(m string) string {
		g := assignmentPattern.FindStringSubmatch(m)
		if g == nil {
			return m
		}
		if _, scheme := schemeOnlyValue[strings.ToLower(g[5])]; scheme {
			return m
		}
		return g[1] + g[2] + g[3] + g[4] + secretMask
	})
	return redactSecretTokens(s)
}

// redactSecretTokens is the shape pass: it walks the string a whitespace-run
// at a time and masks any run that reads as a credential on its own — a known
// prefix, or a long unbroken high-entropy blob.
//
// This is the load-bearing half. The patterns above only fire when someone
// labelled the value; this one fires on a bare `sk-proj-…` sitting in a
// command line with nothing around it to say what it is.
func redactSecretTokens(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))

	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		word := s[start:end]
		if trimmed, lead, trail := trimDelimiters(word); looksLikeSecret(trimmed) {
			b.WriteString(lead)
			b.WriteString(secretMask)
			b.WriteString(trail)
		} else {
			b.WriteString(word)
		}
		start = -1
	}

	for i, r := range s {
		if unicode.IsSpace(r) {
			flush(i)
			b.WriteRune(r)
			continue
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(s))
	return b.String()
}

// trimDelimiters peels the punctuation a shell or a JSON blob wraps a value
// in, so `"sk-abc",` is judged as `sk-abc` and put back with its quotes and
// comma intact.
func trimDelimiters(word string) (trimmed, lead, trail string) {
	const delims = `"'` + "`" + `,;:()[]{}<>`
	trimmed = strings.TrimLeft(word, delims)
	lead = word[:len(word)-len(trimmed)]
	after := strings.TrimRight(trimmed, delims)
	trail = trimmed[len(after):]
	return after, lead, trail
}

// minSecretRunLen is how long an unbroken alphanumeric run has to be before
// its shape alone is taken as evidence. Set well above the longest ordinary
// identifier a command line carries — a git SHA is 40 and a UUID is 36, and
// both are deliberately below this, because masking either would make the
// bubble useless for the case it exists for.
const minSecretRunLen = 44

// looksLikeSecret judges one word.
func looksLikeSecret(word string) bool {
	if len(word) < 8 {
		return false
	}
	// A known prefix is strong evidence, but only on something that is
	// actually a token. `AKIA[0-9A-Z]{16}` is a search PATTERN for AWS keys —
	// the shape an agent types when it is hunting for leaked ones — and
	// masking it hides the very work the bubble exists to report.
	if isTokenRun(word) {
		for _, p := range secretPrefixes {
			if strings.HasPrefix(word, p) && len(word) > len(p)+6 {
				return true
			}
		}
	}
	return isHighEntropyRun(word)
}

// isTokenRun reports whether every character is one a generated credential is
// made of. A regex, a glob, a path with slashes or anything carrying shell
// metacharacters is not a token, whatever it starts with.
func isTokenRun(word string) bool {
	for _, r := range word {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.' || r == '+' || r == '/' || r == '=' || r == '~':
		default:
			return false
		}
	}
	return true
}

// isHighEntropyRun reports whether a word is a long unbroken blob with the
// character mix a random key has and an English word, a path or a URL does
// not.
//
// The tests are all crude on their own and only mean something together:
// long, no separators a human would have put there, and drawn from a mixed
// alphabet. A sentence fails on separators; a path fails on slashes; a hex
// SHA fails on length; a base64 secret passes all three.
func isHighEntropyRun(word string) bool {
	if len(word) < minSecretRunLen {
		return false
	}
	var upper, lower, digit, other int
	for _, r := range word {
		switch {
		case r >= 'A' && r <= 'Z':
			upper++
		case r >= 'a' && r <= 'z':
			lower++
		case r >= '0' && r <= '9':
			digit++
		case r == '_' || r == '-' || r == '.' || r == '+' || r == '/' || r == '=':
			other++
		default:
			// A non-ASCII rune, a slash-heavy path, anything else: this is not
			// a key.
			return false
		}
	}
	// A path or a URL is mostly separators; a key is mostly alphabet.
	if other*3 > len(word) {
		return false
	}
	// Both cases and digits. A lowercase-only run of this length is a sentence
	// with the spaces removed or a long identifier, not a generated key.
	classes := 0
	if upper > 0 {
		classes++
	}
	if lower > 0 {
		classes++
	}
	if digit > 0 {
		classes++
	}
	return classes >= 3
}
