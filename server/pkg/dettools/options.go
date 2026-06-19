package dettools

import (
	"os"
	"strings"
	"time"
)

// DefaultTimeout bounds a single tool invocation when MULTICA_DETTOOLS_TIMEOUT
// is unset or invalid.
const DefaultTimeout = 90 * time.Second

// DefaultArtifactDir is where tools emit artifacts, relative to the task
// working directory.
const DefaultArtifactDir = ".multica/artifacts"

// Options configures a server instance. The daemon sets the matching
// MULTICA_DETTOOLS_* environment variables on the spawned MCP server process;
// OptionsFromEnv reads them here.
type Options struct {
	WorkDir      string
	AllowedTools []string
	Timeout      time.Duration
	AllowNetwork bool
	ArtifactDir  string
	// StepsFile is the path to a JSON array of workspace-authored steps the
	// daemon wrote for this task (MULTICA_DETTOOLS_STEPS_FILE). Empty when the
	// workspace has no authored tools.
	StepsFile string
}

// OptionsFromEnv builds Options from the MULTICA_DETTOOLS_* environment. Missing
// values fall back to safe defaults: the process working directory, the full
// tool catalog, a 90s timeout, network denied, and the default artifact dir.
func OptionsFromEnv() Options {
	workDir := strings.TrimSpace(os.Getenv("MULTICA_DETTOOLS_WORKDIR"))
	if workDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			workDir = cwd
		}
	}

	timeout := DefaultTimeout
	if v := strings.TrimSpace(os.Getenv("MULTICA_DETTOOLS_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			timeout = d
		}
	}

	var allowed []string
	if v := strings.TrimSpace(os.Getenv("MULTICA_DETTOOLS_ALLOWED")); v != "" {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				allowed = append(allowed, p)
			}
		}
	}

	artifactDir := strings.TrimSpace(os.Getenv("MULTICA_DETTOOLS_ARTIFACT_DIR"))
	if artifactDir == "" {
		artifactDir = DefaultArtifactDir
	}

	return Options{
		WorkDir:      workDir,
		AllowedTools: allowed,
		Timeout:      timeout,
		AllowNetwork: boolEnv("MULTICA_DETTOOLS_ALLOW_NETWORK"),
		ArtifactDir:  artifactDir,
		StepsFile:    strings.TrimSpace(os.Getenv("MULTICA_DETTOOLS_STEPS_FILE")),
	}
}

// boolEnv reports whether the named env var is set to a truthy value.
func boolEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}
