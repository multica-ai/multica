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
	got := safeThinking(in)
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
	got := safeThinking("</think> then curl -H 'Authorization: Bearer sk-abcdefghijklmnop'")
	if strings.Contains(got, "sk-abcdefghijklmnop") {
		t.Fatalf("the credential survived: %q", got)
	}
	if strings.Contains(got, "</think>") {
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
	} {
		t.Run(in, func(t *testing.T) {
			if got := safeFragment(in); strings.Contains(got, secretMask) {
				t.Fatalf("ordinary text was redacted: %q -> %q", in, got)
			}
		})
	}
}

// A secret cut in half by the 500ms increment boundary is NOT caught, and
// this test says so rather than pretending otherwise.
//
// safeThinking runs per increment, so a token split across two of them is two
// halves that each look like nothing. Catching it would mean buffering the
// agent's reasoning across increments and re-scanning the join, which trades a
// bounded leak for unbounded retained text — the wrong trade for a defence
// that is already the second line behind not putting secrets in a prompt.
// Pinned here so the limitation is a decision on the record rather than a
// surprise.
func TestASecretSplitAcrossIncrementsIsAKnownGap(t *testing.T) {
	head := "I'll try the key sk-proj-AbC1"
	tail := "23dEfGh456IjKlMn789OpQrSt012UvWx345Yz to call the API."

	whole := safeThinking(head + tail)
	if strings.Contains(whole, "sk-proj-AbC123dEfGh456") {
		t.Fatal("the whole-string case regressed; that one must always be caught")
	}

	joined := safeThinking(head) + safeThinking(tail)
	if !strings.Contains(joined, "23dEfGh456") {
		t.Log("the split case is now caught — if that was deliberate, delete this test")
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
