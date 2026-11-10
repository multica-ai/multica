// Package localenv runs and finds the local environments (dev servers) an
// agent starts for a task, so the daemon can report them on the issue.
//
// An environment is either declared by the repository in
// .multica/environments.toml and supervised by the daemon ("config"), or a
// listening process the daemon finds running inside a task's directory
// ("detected").
package localenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// ConfigPath is where a repository declares its environments, relative to
// the repository root.
const ConfigPath = ".multica/environments.toml"

const (
	DefaultTTL = 24 * time.Hour
	MaxTTL     = 7 * 24 * time.Hour
	minTTL     = time.Minute
)

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// Definition is one [environments.<name>] table.
type Definition struct {
	Name  string
	Label string
	// Start is run by `sh -c` from the repository root.
	Start string
	// URL is where a reader opens the environment. Empty means it has none
	// (a worker, say).
	URL string
	// Ready is polled until it answers; it defaults to URL.
	Ready string
	// Stop, when set, is run instead of signalling the start command's process
	// group. The group is still signalled afterwards if anything is left.
	Stop string
	TTL  time.Duration
}

type configFile struct {
	Environments map[string]struct {
		Label string  `toml:"label"`
		Start string  `toml:"start"`
		URL   *string `toml:"url"`
		Ready string  `toml:"ready"`
		Stop  string  `toml:"stop"`
		TTL   string  `toml:"ttl"`
	} `toml:"environments"`
}

// Config is a repository's parsed environments file.
type Config struct {
	// RepoRoot is the directory holding .multica/; commands run from here.
	RepoRoot     string
	Environments map[string]Definition
}

// Names lists the declared environments in a stable order.
func (c Config) Names() []string {
	names := make([]string, 0, len(c.Environments))
	for name := range c.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ErrNoConfig means no environments file was found between the start
// directory and the boundary.
var ErrNoConfig = errors.New("no " + ConfigPath + " found")

// FindConfig walks up from dir, stopping at boundary, and loads the first
// environments file it meets. dir must be inside boundary.
func FindConfig(dir, boundary string) (Config, error) {
	dir = filepath.Clean(dir)
	boundary = filepath.Clean(boundary)
	if !within(dir, boundary) {
		return Config{}, fmt.Errorf("%s is outside the task directory", dir)
	}
	start := dir
	notFound := fmt.Errorf("%w in %s or its parent directories; run from inside the repository", ErrNoConfig, start)
	for {
		path := filepath.Join(dir, filepath.FromSlash(ConfigPath))
		data, err := os.ReadFile(path)
		if err == nil {
			cfg, err := ParseConfig(data)
			if err != nil {
				return Config{}, fmt.Errorf("%s: %w", path, err)
			}
			cfg.RepoRoot = dir
			return cfg, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return Config{}, err
		}
		if dir == boundary {
			return Config{}, notFound
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Config{}, notFound
		}
		dir = parent
	}
}

// ParseConfig parses and validates an environments file.
func ParseConfig(data []byte) (Config, error) {
	var file configFile
	if err := toml.Unmarshal(data, &file); err != nil {
		return Config{}, err
	}
	cfg := Config{Environments: make(map[string]Definition, len(file.Environments))}
	for name, raw := range file.Environments {
		if !nameRE.MatchString(name) {
			return Config{}, fmt.Errorf("environment name %q must be lowercase letters, digits, - or _ (at most 40)", name)
		}
		def := Definition{
			Name:  name,
			Label: strings.TrimSpace(raw.Label),
			Start: strings.TrimSpace(raw.Start),
			URL:   "http://localhost:$MULTICA_PORT",
			Ready: strings.TrimSpace(raw.Ready),
			Stop:  strings.TrimSpace(raw.Stop),
			TTL:   DefaultTTL,
		}
		if def.Start == "" {
			return Config{}, fmt.Errorf("environment %q has no start command", name)
		}
		if raw.URL != nil {
			def.URL = strings.TrimSpace(*raw.URL)
		}
		if def.Ready == "" {
			def.Ready = def.URL
		}
		if ttl := strings.TrimSpace(raw.TTL); ttl != "" {
			d, err := time.ParseDuration(ttl)
			if err != nil {
				return Config{}, fmt.Errorf("environment %q ttl: %w", name, err)
			}
			if d < minTTL || d > MaxTTL {
				return Config{}, fmt.Errorf("environment %q ttl must be between %s and %s", name, minTTL, MaxTTL)
			}
			def.TTL = d
		}
		cfg.Environments[name] = def
	}
	if len(cfg.Environments) == 0 {
		return Config{}, errors.New("declares no environments")
	}
	return cfg, nil
}

// expandPort substitutes $MULTICA_PORT / ${MULTICA_PORT} in a URL. Commands
// are not expanded here: the shell sees MULTICA_PORT in their environment.
func expandPort(s string, port int) string {
	return os.Expand(s, func(key string) string {
		if key == "MULTICA_PORT" {
			return fmt.Sprint(port)
		}
		return "$" + key
	})
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
