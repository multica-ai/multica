package redact

import (
	"strings"
	"testing"
)

func TestFindReportsEachRule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, input, rule, secret string
	}{
		{"github classic", "export GITHUB_TOKEN=" + asm("ghp_", "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"), RuleGitHubToken, asm("ghp_", "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij")},
		{"github fine-grained", "token " + asm("github_", "pat_", "11ABCDE0Q0abcdefghijkl_MNOP"), RuleGitHubToken, asm("github_", "pat_", "11ABCDE0Q0abcdefghijkl_MNOP")},
		{"anthropic", "key: " + asm("sk-", "ant-", "api03-abcdefghijklmnopqrstu"), RuleAnthropicKey, asm("sk-", "ant-", "api03-abcdefghijklmnopqrstu")},
		{"openai", "key " + asm("sk-", "proj-", "Rf8vK2nQ0wLx4ZtY7bMcP3sH"), RuleOpenAIKey, asm("sk-", "proj-", "Rf8vK2nQ0wLx4ZtY7bMcP3sH")},
		{"aws access", "id " + asm("AKIA", "IOSFODNN7EXAMPLE"), RuleAWSAccessKey, asm("AKIA", "IOSFODNN7EXAMPLE")},
		{"aws secret value only", "aws_secret_access_key = " + asm("wJalrXUtnFEMI/K7MDENG/", "bPxRfiCYEXAMPLEKEY"), RuleAWSSecretKey, asm("wJalrXUtnFEMI/K7MDENG/", "bPxRfiCYEXAMPLEKEY")},
		{"google trailing dash", `"` + asm("AIza", "SyB1cD3fGhIjKlMnOpQrStUvWxYz012345-") + `"`, RuleGoogleAPIKey, asm("AIza", "SyB1cD3fGhIjKlMnOpQrStUvWxYz012345-")},
		{"stripe", "use " + asm("sk_", "live_", "abcdefghijklmnop1234"), RuleStripeKey, asm("sk_", "live_", "abcdefghijklmnop1234")},
		{"multica", "MULTICA_TOKEN " + asm("mul_", strings.Repeat("ab12", 10)), RuleMulticaToken, asm("mul_", strings.Repeat("ab12", 10))},
		{"connection password only", "DATABASE_URL=postgres://app:" + asm("hunter", "2pass") + "@db.internal:5432/orders", RuleConnectionPassword, asm("hunter", "2pass")},
		{"private key", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----", RulePrivateKey, "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ms := Find(tc.input)
			if len(ms) != 1 {
				t.Fatalf("Find(%q) = %+v, want one match", tc.input, ms)
			}
			m := ms[0]
			if m.Rule != tc.rule || m.Value != tc.secret || tc.input[m.Start:m.End] != tc.secret {
				t.Fatalf("Find(%q) = %+v, want rule %s secret %q", tc.input, m, tc.rule, tc.secret)
			}
		})
	}
}

func TestFindSkipsNoisyRules(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"PASSWORD=changeme",
		"Authorization: Bearer abcdefghijklmnop",
		"jwt " + asm("eyJhbGciOiJIUzI1", ".eyJzdWIiOiIxMjM0", ".SflKxwRJSMeKKF2QT4"),
		"the sk-learn package",
		"publishable " + asm("pk_", "live_", "abcdefghijklmnop1234"),
	} {
		if ms := Find(input); len(ms) != 0 {
			t.Fatalf("Find(%q) = %+v, want nothing", input, ms)
		}
	}
}

func TestFindOrdersAndReplaces(t *testing.T) {
	t.Parallel()
	gh := asm("ghp_", "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij")
	stripe := asm("sk_", "live_", "abcdefghijklmnop1234")
	input := "stripe " + stripe + " then github " + gh + " and again " + gh
	ms := Find(input)
	if len(ms) != 3 || ms[0].Rule != RuleStripeKey || ms[1].Rule != RuleGitHubToken || ms[2].Value != gh {
		t.Fatalf("Find = %+v, want stripe then two github matches in order", ms)
	}
	got := Replace(input, ms)
	want := "stripe [REDACTED STRIPE KEY] then github [REDACTED GITHUB TOKEN] and again [REDACTED GITHUB TOKEN]"
	if got != want {
		t.Fatalf("Replace = %q, want %q", got, want)
	}

	conn := "postgres://app:" + asm("hunter", "2pass") + "@db"
	if got := Replace(conn, Find(conn)); got != "postgres://app:[REDACTED PASSWORD]@db" {
		t.Fatalf("Replace(connection string) = %q", got)
	}
}

func TestMask(t *testing.T) {
	t.Parallel()
	mask := func(input string) string {
		ms := Find(input)
		if len(ms) != 1 {
			t.Fatalf("Find(%q) = %+v, want one match", input, ms)
		}
		return Mask(ms[0])
	}
	for _, tc := range []struct{ input, want string }{
		{asm("ghp_", "7Hq2LmZx9VbR4tKp1sWcY8nDf3GjU6aE0oQi"), "ghp_••••••••0oQi"},
		{asm("sk-", "proj-", "Rf8vK2nQ0wLx4ZtY7bMcP3sH9dJ6aE1uX7aQ"), "sk-proj-••••••••X7aQ"},
		{asm("AKIA", "IOSFODNN7EXAMPLE"), "AKIA••••••••MPLE"},
		{asm("glpat-", "abcdefghijklmnopqrst"), "glpat-••••••••qrst"},
		{"postgres://app:" + asm("my_", "secret_pass") + "@db", "••••"},
		{"-----BEGIN OPENSSH PRIVATE KEY-----\nb3Bl\n-----END OPENSSH PRIVATE KEY-----", "-----BEGIN PRIVATE KEY----- ••••"},
	} {
		if got := mask(tc.input); got != tc.want {
			t.Fatalf("Mask(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
