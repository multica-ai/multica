package wecom

// progress_secrets_test.go — what must never reach a bubble, and what must
// still reach it.
//
// Both halves matter. WeCom has no edit and no unsend and the tenant archives
// every message, so a credential that lands once is in a corporate record for
// good. But the bubble exists to say what the agent is doing, and a redactor
// that eats paths, SHAs and URLs makes it say nothing.

import (
	"strings"
	"testing"
)

func TestACredentialNeverReachesTheBubble(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     string
		gone   string // must not survive
		intact string // must survive, so the reader still learns something
	}{
		{
			name:   "bearer header in a curl",
			in:     `bash -lc 'curl -H "Authorization: Bearer sk-proj-abcdefghijklmnop" https://api.example.com/v1/me'`,
			gone:   "sk-proj-abcdefghijklmnop",
			intact: "api.example.com",
		},
		{
			name:   "mcp tool argument",
			in:     `api_token=ghp_ABCdefGHIjklMNOpqrSTUvwxYZ0123456789, project=ACME`,
			gone:   "ghp_ABCdefGHIjklMNOpqrSTUvwxYZ0123456789",
			intact: "ACME",
		},
		{
			name:   "password in a clone url",
			in:     `git clone https://alex:hunter2@git.example.com/acme/web.git`,
			gone:   "hunter2",
			intact: "git.example.com/acme/web.git",
		},
		{
			name:   "a short password nothing about its shape would flag",
			in:     `psql "password=s3cr3t host=db.internal"`,
			gone:   "s3cr3t",
			intact: "db.internal",
		},
		{
			name:   "long flag",
			in:     `deploy --token=AbCdEf0123456789XyZ --env=prod`,
			gone:   "AbCdEf0123456789XyZ",
			intact: "--env=prod",
		},
		{
			name:   "aws key with no label at all",
			in:     `aws s3 ls --profile AKIAIOSFODNN7EXAMPLE`,
			gone:   "AKIAIOSFODNN7EXAMPLE",
			intact: "aws s3 ls",
		},
		{
			name:   "a jwt sitting bare in the line",
			in:     `curl -H eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.abc https://x.example`,
			gone:   "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			intact: "x.example",
		},
		{
			name:   "high-entropy blob with no prefix anyone has written down",
			in:     `sync --key Zm9vYmFyMTIzNDU2Nzg5MFFXRVJUWXVpb3BBU0RGR0hqa2wxMjM0 --dry-run`,
			gone:   "Zm9vYmFyMTIzNDU2Nzg5MFFXRVJUWXVpb3BBU0RGR0hqa2wxMjM0",
			intact: "--dry-run",
		},
		{
			name:   "quoted json value",
			in:     `{"client_secret": "abc123def456ghi789", "scope": "read"}`,
			gone:   "abc123def456ghi789",
			intact: "read",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := safeFragment(tc.in)
			if strings.Contains(got, tc.gone) {
				t.Fatalf("the credential reached the bubble: %q", got)
			}
			if !strings.Contains(got, tc.intact) {
				t.Fatalf("the line no longer says what the agent is doing: %q", got)
			}
			if !strings.Contains(got, secretMask) {
				t.Fatalf("the value vanished with no sign it was redacted: %q", got)
			}
		})
	}
}

// The other half: a redactor that eats ordinary arguments makes the bubble
// useless for the case it exists for. A git SHA and a UUID are both
// deliberately under the length threshold.
func TestOrdinaryArgumentsSurviveIntact(t *testing.T) {
	for _, in := range []string{
		`Read server/internal/integrations/wecom/progress_render.go`,
		`git show 3d80274ab1f9c0e4d5a6b7c8d9e0f1a2b3c4d5e6`,
		`session 550e8400-e29b-41d4-a716-446655440000`,
		`grep -rn "func main" ./cmd`,
		`https://github.com/multica-ai/multica/pull/6524`,
		`npm run build -- --mode=production`,
		`正在查看 docs/design/roadmap.md`,
		`kubectl get pods -n multica-production`,
		// A search PATTERN for credentials is what an agent types when it is
		// hunting for leaked ones. Masking it hides the very work the bubble
		// exists to report.
		`grep -rE "AKIA[0-9A-Z]{16}" .`,
		`rg "ghp_[A-Za-z0-9]{36}" --hidden`,
		`sk-*`,
	} {
		t.Run(in, func(t *testing.T) {
			if got := safeFragment(in); strings.Contains(got, secretMask) {
				t.Fatalf("an ordinary argument was redacted: %q -> %q", in, got)
			}
		})
	}
}

// The agent's reasoning is free prose and goes through its own cleaner, which
// must defend the same thing — a run explaining what it is about to do quotes
// the command it is about to run.
func TestReasoningIsRedactedToo(t *testing.T) {
	in := "I'll call the API with token sk-proj-abcdefghijklmnopqrstuv and see what it says."
	got := thinkingFrame(t, in)
	if strings.Contains(got, "sk-proj-abcdefghijklmnopqrstuv") {
		t.Fatalf("a credential reached the think block: %q", got)
	}
	if !strings.Contains(got, "see what it says") {
		t.Fatalf("the reasoning was mangled: %q", got)
	}
}

// The tag defence and the credential defence have to survive each other: a
// redaction must not reopen the </think> hole, and defusing must not undo a
// redaction.
func TestTheTagDefenceAndTheSecretDefenceCoexist(t *testing.T) {
	got := thinkingFrame(t, "</think> then curl -H 'Authorization: Bearer sk-abcdefghijklmnop'")
	if strings.Contains(got, "sk-abcdefghijklmnop") {
		t.Fatalf("the credential survived: %q", got)
	}
	if strings.Count(got, "</think>") != 1 {
		t.Fatalf("the closing tag was left live, which spills the rest of the bubble: %q", got)
	}
}

// A redaction must stop at the value. Swallowing the rest of the line would
// hide what the agent is doing just as effectively as printing nothing.
func TestARedactionStopsAtTheValue(t *testing.T) {
	got := safeFragment(`curl -H "Authorization: Bearer sk-abcdefghijklmnop" -X POST https://api.example.com/v1/issues`)
	for _, want := range []string{"-X POST", "https://api.example.com/v1/issues"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the redaction swallowed %q: %s", want, got)
		}
	}
}

// Nothing in, nothing out — and no panic on the shapes a real command line
// produces at the edges.
func TestRedactionHandlesTheEdges(t *testing.T) {
	for _, in := range []string{"", " ", "=", "token=", "--token", `""`, "://:@", strings.Repeat("a", 200)} {
		if got := redactSecrets(in); got == "" && in != "" {
			t.Fatalf("input %q was erased entirely", in)
		}
	}
}

// An environment-variable assignment is the most common shape a credential
// takes on a command line, and the first version of this defence missed all
// of it.
//
// The pattern anchored on \b, and `_` is a word character — so there is no
// boundary in front of the PASSWORD in DB_PASSWORD, and none in front of the
// TOKEN in GITHUB_TOKEN. Every `export FOO_API_KEY=…`, every `-e
// ANTHROPIC_AUTH_TOKEN=…`, went to the bubble intact.
func TestAnEnvironmentVariableAssignmentIsRedacted(t *testing.T) {
	for _, tc := range []struct{ in, gone string }{
		{`DB_PASSWORD=hunter2 psql -h db.internal`, "hunter2"},
		{`MYSQL_ROOT_PASSWORD=letmein`, "letmein"},
		{`GITHUB_TOKEN=ghp_abc123def456 gh pr list`, "ghp_abc123def456"},
		{`export OPENAI_API_KEY=sk-short`, "sk-short"},
		{`-e ANTHROPIC_AUTH_TOKEN=xyz789abc`, "xyz789abc"},
		{`docker run -e SECRET_KEY=abc123 img`, "abc123"},
		{`refresh_token=1//0gABCdefGhi`, "1//0gABCdefGhi"},
		{`kubectl create secret generic db --from-literal=password=s3cr3t`, "s3cr3t"},
		// The secret word ending a key with nothing to mark the join.
		{`dbpassword=hunter2 ./migrate`, "hunter2"},
		{`MYSQLPASSWORD=letmein`, "letmein"},
		{`{"userToken": "abc123xyz"}`, "abc123xyz"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got := safeFragment(tc.in)
			if strings.Contains(got, tc.gone) {
				t.Fatalf("the credential reached the bubble: %q", got)
			}
			if !strings.Contains(got, secretMask) {
				t.Fatalf("the value vanished with no sign it was redacted: %q", got)
			}
		})
	}
}

// `curl -u user:password` carries the secret in an argument with no key name
// at all.
func TestBasicAuthOnACommandLineIsRedacted(t *testing.T) {
	got := safeFragment(`curl -u admin:admin123 https://api.example.com/v1/me`)
	if strings.Contains(got, "admin123") {
		t.Fatalf("the password reached the bubble: %q", got)
	}
	if !strings.Contains(got, "admin") || !strings.Contains(got, "api.example.com") {
		t.Fatalf("the line no longer says what the agent is doing: %q", got)
	}
}

// The auth scheme is the one part of an Authorization header worth keeping.
// An earlier version let the assignment rule take "Bearer" as the value, so
// the line read "Authorization: ███ ███" and lost which scheme was in use.
func TestTheAuthSchemeSurvivesTheRedaction(t *testing.T) {
	got := safeFragment(`curl -H "Authorization: Bearer sk-abcdefghijklmnop" https://x.example`)
	if strings.Contains(got, "sk-abcdefghijklmnop") {
		t.Fatalf("the token survived: %q", got)
	}
	if !strings.Contains(got, "Bearer") {
		t.Fatalf("the scheme was redacted along with the token: %q", got)
	}
}

// The widened key pattern must not start eating ordinary text. These are the
// exact shapes that broke when the anchor was loosened.
func TestTheWiderKeyPatternDoesNotEatProse(t *testing.T) {
	for _, in := range []string{
		`AUTHORS=file.txt`,
		`git commit -m "auth: fix the redirect"`,
		`vim server/internal/auth/handler.go`,
		`the authentication flow is broken`,
		`SELECT * FROM tokens WHERE id=1`,
		`grep -rn "auth:" .`,
		`ssh -i ~/.ssh/id_rsa user@host`,
		// The secret word must be followed by the separator, not merely
		// start a longer word.
		`tokenizer=bpe`,
		`passport=GB`,
		`PWD=/srv/app`,
	} {
		t.Run(in, func(t *testing.T) {
			if got := safeFragment(in); strings.Contains(got, secretMask) {
				t.Fatalf("ordinary text was redacted: %q -> %q", in, got)
			}
		})
	}
}

// A secret cut in half by the 500ms increment boundary is caught, in every
// frame the bubble is sent, not only the last one.
//
// The increments are joined in the feed's buffer, and redaction runs over the
// join. Redacting each increment instead fails both ways shown here: a value
// arriving after its key is two halves that each look like nothing, and a key
// whose prefix arrives alone has that half masked while the rest — too short
// to look like a secret on its own — goes out in the clear.
//
// REVERSE VERIFICATION: move redactSecrets from tidyThinking back into
// safeThinking, and both cases fail.
func TestASecretSplitAcrossIncrementsIsCaught(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []string
		secret string
	}{
		{
			name:   "a value arriving after its key",
			chunks: []string{"I'll connect with password=", "hunter2 and see."},
			secret: "hunter2",
		},
		{
			name:   "a key split after its prefix",
			chunks: []string{"I'll try the key sk-proj-AbC1", "23dEfGh456IjKlMn789OpQrSt012UvWx345Yz to call the API."},
			secret: "23dEfGh456",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, frame := range thinkingFrames(t, tc.chunks...) {
				if strings.Contains(frame, tc.secret) {
					t.Errorf("frame %d carries %q: %q", i, tc.secret, frame)
				}
			}
		})
	}
}

// A URL is one word to the shape pass, and fails it on its slashes. What it
// carries still has to be found: a key as one of its parameters, a presigned
// URL's signature, and a webhook whose path is the credential.
//
// REVERSE VERIFICATION: make redactURLParts return the word unchanged, drop
// signature from assignmentPattern, drop webhookPattern or queryParamPattern,
// and the matching case fails.
func TestACredentialInsideAURLIsRedacted(t *testing.T) {
	for _, tc := range []struct{ in, gone string }{
		{`curl "https://api.example.com/v1/data?access=sk-proj-abcdefghijklmnop&page=2"`, "sk-proj-abcdefghijklmnop"},
		{`curl https://bucket.s3.amazonaws.com/report.pdf?X-Amz-Expires=600&X-Amz-Signature=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef`, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{`curl -X POST https://hooks.slack.com/services/T0001/B0002/AbCdEfGhIjKl -d @msg.json`, "AbCdEfGhIjKl"},
		{`post to https://discord.com/api/webhooks/1234567/aBcDeFgH-ijkl`, "aBcDeFgH-ijkl"},
		{`curl https://open.feishu.cn/open-apis/bot/v2/hook/0a1b2c3d-e4f5`, "0a1b2c3d-e4f5"},
		{`curl 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=693a91f6-7xxx' -d @m.json`, "693a91f6-7xxx"},
		{`curl https://oapi.dingtalk.com/robot/send?access_token=abc123def`, "abc123def"},
		{`GET /oauth/callback?state=xyz&code=4/0AbCdEf`, "4/0AbCdEf"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got := safeFragment(tc.in)
			if strings.Contains(got, tc.gone) {
				t.Fatalf("the credential reached the bubble: %q", got)
			}
		})
	}
	// And an ordinary URL is left readable: its parts are a host and a path.
	for _, in := range []string{
		`open https://github.com/multica-ai/multica/pull/9155`,
		`curl https://api.example.com/v1/users/7f3c2a9e-1b4d-4e8a-9c2f-3d5e6f7a8b9c?expand=teams`,
	} {
		if got := safeFragment(in); strings.Contains(got, secretMask) {
			t.Errorf("an ordinary URL was redacted: %q -> %q", in, got)
		}
	}
}

// A bare key with no prefix is judged on its shape, and its shape is not
// there until all of it has arrived. Every frame is checked, because every
// frame is sent: the one rendered while the key was half written is the one
// that would carry it.
//
// REVERSE VERIFICATION: drop the hold-back in tidyThinking and the frames
// rendered mid-key carry its first characters.
func TestABareKeyStreamedAcrossIncrementsNeverShows(t *testing.T) {
	key := strings.Repeat("aB3dE5gH7jK9", 5) // 60 characters, all three classes
	chunks := []string{"I'll export the key "}
	for i := 0; i < len(key); i += 10 {
		chunks = append(chunks, key[i:i+10])
	}
	chunks = append(chunks, " and then run the tests.")

	frames := thinkingFrames(t, chunks...)
	for i, frame := range frames {
		for j := 0; j+8 <= len(key); j++ {
			if strings.Contains(frame, key[j:j+8]) {
				t.Fatalf("frame %d carries %q, eight characters of the key: %q", i, key[j:j+8], frame)
			}
		}
	}
	if last := frames[len(frames)-1]; !strings.Contains(last, "run the tests.") {
		t.Errorf("an ordinary last word was held back as if it might be a key: %q", last)
	}
}

// Where the raw buffer is cut inside a word, that word goes: what is left of a
// key, or a value cut away from its key name, is no longer recognisable.
func TestTheRawReasoningIsNotCutInsideAWord(t *testing.T) {
	t.Parallel()
	s := "connect with password=hunter2 and then 甲乙丙"
	n := len([]rune(s)) - len([]rune("connect with pass"))
	got := cutRawThinking(s, n)
	if strings.Contains(got, "hunter2") {
		t.Errorf("the cut kept a value whose key it had sliced off: %q", got)
	}
	if !strings.Contains(got, "and then 甲乙丙") {
		t.Errorf("the cut took more than the word it landed in: %q", got)
	}

	// The first whole word after the cut is still judged on its own: the
	// marker the cut leaves must not hide a key behind it.
	key := `"sk-proj-abcdefghijklmnop"`
	s = "xx" + key + " and then"
	got = redactSecrets(cutRawThinking(s, len([]rune(s))-1))
	if strings.Contains(got, "sk-proj-abcdefghijklmnop") {
		t.Errorf("a key right after the cut escaped redaction: %q", got)
	}
}

// A cut that takes a value's name leaves the value with nothing to say what it
// is: `: hunter2` reads as nothing to the assignment rule and is too short for
// the shape pass. So a separator right after the cut takes its value with it,
// however the pair was written.
//
// REVERSE VERIFICATION: stop dropping the orphaned value and every case fails.
func TestACutThatTakesAValuesNameTakesTheValueToo(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"connect with password: hunter2 and then 甲乙丙",
		"connect with password = 'hunter2' and then 甲乙丙",
		`connect with "password": "hunter2" and then 甲乙丙`,
	} {
		n := len([]rune(s)) - len([]rune(s[:strings.Index(s, "word")]))
		got := redactSecrets(cutRawThinking(s, n))
		if strings.Contains(got, "hunter2") {
			t.Errorf("cut %q: the value survived without its name: %q", s, got)
		}
		if !strings.Contains(got, "and then 甲乙丙") {
			t.Errorf("cut %q: took more than the name and its value: %q", s, got)
		}
	}
}

// TestTheSecretRunLengthStaysAboveTheOrdinaryIdentifiers pins minSecretRunLen,
// which nothing else does: every other test here feeds strings built from the
// constant, so it walks wherever the constant walks and a value of 8 would
// leave them all green while the bubble started masking every commit hash.
//
// The number is a trade with two ways to be wrong, and the file already names
// both, so both are asserted rather than the value itself. Too low and the
// identifiers a person is watching the bubble FOR — the SHA being checked out,
// the UUID being looked up — come back as asterisks. Too high and a run long
// enough to be a credential is printed into a group chat.
func TestTheSecretRunLengthStaysAboveTheOrdinaryIdentifiers(t *testing.T) {
	t.Parallel()
	const gitSHA, uuid = 40, 36
	if minSecretRunLen <= gitSHA {
		t.Errorf("minSecretRunLen = %d, at or below a git SHA's %d; the bubble would mask the one identifier its reader is most likely to be following", minSecretRunLen, gitSHA)
	}
	if minSecretRunLen <= uuid {
		t.Errorf("minSecretRunLen = %d, at or below a UUID's %d", minSecretRunLen, uuid)
	}
	if minSecretRunLen > 64 {
		t.Errorf("minSecretRunLen = %d; past 64 an ordinary API token is printed into the room on shape alone", minSecretRunLen)
	}

	// And the behaviour the number is there for, driven rather than described.
	sha := strings.Repeat("a1b2", gitSHA/4)
	if looksLikeSecret(sha) {
		t.Errorf("a %d-character git SHA was judged a secret", len(sha))
	}
	long := strings.Repeat("aB3", 20) // 60 characters, all three classes, no separators
	if !looksLikeSecret(long) {
		t.Errorf("an %d-character unbroken run was not judged a secret", len(long))
	}
}

// A key glued to the shell around it is still a key. The shape pass splits on
// whitespace, and a word like `sk-proj-…|base64` is not a token as a whole, so
// each piece between shell punctuation is judged on its own.
//
// REVERSE VERIFICATION: split words on the URL delimiters alone again, and the
// pipe, semicolon, redirect and $( cases fail.
func TestACredentialGluedToShellPunctuationIsRedacted(t *testing.T) {
	const key = "sk-proj-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"
	const gh = "ghp_AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"
	for _, tc := range []struct{ name, in, gone string }{
		{"pipe", "echo " + key + "|base64", key},
		{"semicolon", "export K=x;echo " + key + ";ls", key},
		{"redirect", "echo " + gh + ">token.txt", gh},
		{"command substitution", "x=$(" + gh + ")", gh},
		{"backtick", "x=`" + gh + "`", gh},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeFragment(tc.in); strings.Contains(got, tc.gone) {
				t.Errorf("safeFragment(%q) = %q, the key is still there", tc.in, got)
			}
		})
	}
}

// What the bubble shows is what the redactor judged. A character the renderer
// removes must not join two pieces into a key: a control character inside a
// key is dropped before the redactor reads it, so it sees the whole key, and
// an angle bracket becomes a space after it, so the halves stay apart.
//
// REVERSE VERIFICATION: drop control characters after the redactor, or delete
// the markup characters instead of spacing them, and the matching case fails.
func TestNothingTheRendererDropsJoinsAKey(t *testing.T) {
	// No known prefix: only its length and mix give it away, and each half
	// on its own is too short to judge.
	const key = "Zx9Qw3Er7Ty1Ui5Op2As8Df4Gh6Jk0Lm3Nb7Vc1Xz5Qa9Ws2EdRf"
	for _, tc := range []struct{ name, in string }{
		{"control character inside", "echo " + key[:26] + "\x01" + key[26:]},
		{"angle bracket inside", "echo " + key[:26] + "<" + key[26:]},
		{"ampersand inside", "echo " + key[:26] + "&" + key[26:]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeFragment(tc.in); strings.Contains(got, key) {
				t.Errorf("safeFragment(%q) = %q, shows a key the redactor never saw whole", tc.in, got)
			}
		})
	}
}
