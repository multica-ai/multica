package internalbrowserqa

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

type Credential struct {
	Username     string
	Password     string
	SessionToken string
}

type Target struct {
	Name             string
	URL              string
	Vault            string
	UsernameSelector string
	PasswordSelector string
	SubmitSelector   string
	ExpectedText     []string
	SessionCookie    bool
}

func (t Target) Host() string {
	parsed, err := url.Parse(t.URL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

var targets = map[string]Target{
	"multica": {
		Name: "multica", URL: "http://multica.internal:3000/login",
		ExpectedText: []string{"Issues", "Agents"}, SessionCookie: true,
	},
	"cerebro": {
		Name: "cerebro", URL: "http://multica-staging-web.internal:3000/login",
		ExpectedText: []string{"Issues", "Agents"}, SessionCookie: true,
	},
	"registry": {
		Name: "registry", URL: "http://firtal-data-registry-private.internal:3000/auth/login?manual=true",
		Vault: "Shared/browser-login/registry", UsernameSelector: "#email", PasswordSelector: "#password",
		SubmitSelector: "button[type=submit]", ExpectedText: []string{"Dashboard", "Data Sources"},
	},
	"finance": {
		Name: "finance", URL: "http://firtal-internal-private.internal:3000/login?manual=true",
		Vault: "Shared/browser-login/finance", UsernameSelector: "#email", PasswordSelector: "#password",
		SubmitSelector: "button[type=submit]", ExpectedText: []string{"Financial overview"},
	},
	"pricing": {
		Name: "pricing", URL: "http://ecommerce-pricing-engine-private.internal:3000/login?manual=true",
		Vault: "Shared/browser-login/pricing", UsernameSelector: "#email", PasswordSelector: "#password",
		SubmitSelector: "button[type=submit]", ExpectedText: []string{"Dashboard"},
	},
	"customer-service": {
		Name: "customer-service", URL: "http://customer-service.internal:3456/desk",
		ExpectedText: []string{"Desk", "Analytics"},
	},
}

func TargetFor(name string) (Target, error) {
	target, ok := targets[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return Target{}, fmt.Errorf("internal browser target is not allowed")
	}
	parsed, err := url.Parse(target.URL)
	if err != nil || parsed.Scheme != "http" || !strings.Contains(parsed.Hostname(), ".internal") {
		return Target{}, fmt.Errorf("internal browser target is misconfigured")
	}
	return target, nil
}

type Commander interface {
	Run(ctx context.Context, stdin string, args ...string) ([]byte, error)
}

type ExecCommander struct{}

func (ExecCommander) Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "agent-browser", args...)
	if stdin != "" {
		command.Stdin = strings.NewReader(stdin)
	}
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("agent-browser command failed")
	}
	return output, nil
}

type Result struct {
	App          string   `json:"app"`
	InternalHost string   `json:"internal_host"`
	FinalURL     string   `json:"final_url"`
	Markers      []string `json:"markers"`
	Errors       []string `json:"errors"`
}

type Runner struct {
	commander Commander
}

func NewRunner(commander Commander) *Runner {
	return &Runner{commander: commander}
}

func (r *Runner) runStage(ctx context.Context, stage, stdin string, args ...string) ([]byte, error) {
	output, err := r.commander.Run(ctx, stdin, args...)
	if err != nil {
		return nil, fmt.Errorf("internal browser stage %s failed", stage)
	}
	return output, nil
}

func SafeError(err error) string {
	message := err.Error()
	switch message {
	case "internal browser stage open failed",
		"internal browser stage auth failed",
		"internal browser stage reload failed",
		"internal browser stage snapshot failed",
		"internal browser stage markers failed",
		"internal browser stage url failed",
		"internal browser stage errors failed":
		return message
	}
	return "internal browser verification failed"
}

func (r *Runner) Verify(ctx context.Context, app string, credential Credential) (Result, error) {
	target, err := TargetFor(app)
	if err != nil {
		return Result{}, err
	}
	hasPassword := credential.Username != "" || credential.Password != ""
	if target.Vault == "" && hasPassword {
		return Result{}, fmt.Errorf("target does not accept a browser credential")
	}
	if target.Vault != "" && (!hasPassword || credential.Username == "" || credential.Password == "") {
		return Result{}, fmt.Errorf("target requires a complete browser credential")
	}
	if target.SessionCookie != (credential.SessionToken != "") {
		return Result{}, fmt.Errorf("target session credential does not match its auth mode")
	}

	session, err := sessionName()
	if err != nil {
		return Result{}, err
	}
	baseArgs := []string{"--session", session}
	defer func() { _, _ = r.commander.Run(context.Background(), "", append(baseArgs, "close")...) }()

	if _, err := r.runStage(ctx, "open", "", append(baseArgs, "open", target.URL)...); err != nil {
		return Result{}, err
	}
	if target.Vault != "" || target.SessionCookie {
		commands := make([][]string, 0, 4)
		if target.Vault != "" {
			commands = append(commands,
				[]string{"fill", target.UsernameSelector, credential.Username},
				[]string{"fill", target.PasswordSelector, credential.Password},
				[]string{"click", target.SubmitSelector},
			)
		}
		if target.SessionCookie {
			parsed, _ := url.Parse(target.URL)
			rootURL := parsed.Scheme + "://" + parsed.Host + "/"
			commands = append(commands,
				[]string{"cookies", "set", "multica_auth", credential.SessionToken, "--url", rootURL, "--httpOnly", "--sameSite", "Strict"},
				[]string{"open", rootURL},
			)
		}
		commands = append(commands, []string{"wait", "1500"})
		payload, _ := json.Marshal(commands)
		// This output is deliberately discarded: the stdin payload contains secrets.
		if _, err := r.runStage(ctx, "auth", string(payload), append(baseArgs, "batch")...); err != nil {
			return Result{}, err
		}
	}
	if _, err := r.runStage(ctx, "reload", "", append(baseArgs, "reload")...); err != nil {
		return Result{}, err
	}
	snapshot, err := r.runStage(ctx, "snapshot", "", append(baseArgs, "snapshot")...)
	if err != nil {
		return Result{}, err
	}
	for _, marker := range target.ExpectedText {
		if !strings.Contains(string(snapshot), marker) {
			return Result{}, fmt.Errorf("internal browser stage markers failed")
		}
	}
	finalURL, err := r.runStage(ctx, "url", "", append(baseArgs, "get", "url")...)
	if err != nil {
		return Result{}, err
	}
	rawErrors, err := r.runStage(ctx, "errors", "", append(baseArgs, "errors")...)
	if err != nil {
		return Result{}, err
	}
	errors := decodeErrors(rawErrors)
	return Result{
		App: target.Name, InternalHost: target.Host(), FinalURL: strings.TrimSpace(string(finalURL)),
		Markers: append([]string(nil), target.ExpectedText...), Errors: errors,
	}, nil
}

func sessionName() (string, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("create browser session")
	}
	return "internal-qa-" + hex.EncodeToString(suffix[:]), nil
}

func decodeErrors(raw []byte) []string {
	var errors []string
	if json.Unmarshal(raw, &errors) == nil {
		return errors
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "[]" {
		return []string{}
	}
	return []string{"browser reported errors"}
}
