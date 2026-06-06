package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	skillpkg "github.com/multica-ai/multica/server/internal/skill"
)

// maxDiscoverCandidates caps how many skills one discovery call returns.
// Mirrors MAX_CANDIDATES on the client (packages/views/skills/lib/folder-discovery.ts).
const maxDiscoverCandidates = 100

// SkillCandidate is one discovered skill the client can import by URL.
type SkillCandidate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	ImportURL   string `json:"import_url"`
}

// SkillDiscoveryResponse is returned by POST /api/skills/discover.
type SkillDiscoveryResponse struct {
	Candidates []SkillCandidate `json:"candidates"`
	Truncated  bool             `json:"truncated"`
}

// selectSkillDirsFromTree returns the directory of every SKILL.md blob in the
// tree, scoped under `scope` (""=whole repo). "" means a root SKILL.md.
func selectSkillDirsFromTree(tree []githubTreeEntry, scope string) []string {
	scope = strings.Trim(scope, "/")
	dirs := make([]string, 0)
	for _, e := range tree {
		if e.Type != "blob" {
			continue
		}
		if e.Path != "SKILL.md" && !strings.HasSuffix(e.Path, "/SKILL.md") {
			continue
		}
		dir := strings.TrimSuffix(strings.TrimSuffix(e.Path, "SKILL.md"), "/")
		if scope != "" && dir != scope && !strings.HasPrefix(dir, scope+"/") {
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

// discoverGitHubSkills resolves the repo/ref, fetches the recursive tree, and
// for each SKILL.md reads its frontmatter to build a candidate list. Only the
// SKILL.md bodies are fetched here — supporting files are fetched at import
// time by the existing single-skill importer.
func (h *Handler) discoverGitHubSkills(rawURL string) (*SkillDiscoveryResponse, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}

	spec, err := parseGitHubURL(rawURL)
	if err != nil {
		return nil, err
	}
	if len(spec.refSegments) > 0 {
		if err := resolveGitHubRefAndPath(httpClient, &spec); err != nil {
			return nil, err
		}
	}
	if spec.ref == "" {
		spec.ref = fetchGitHubDefaultBranch(httpClient, spec.owner, spec.repo)
	}

	treeURL := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1",
		spec.owner, spec.repo, escapeRefPath(spec.ref),
	)
	resp, err := doGitHubAPIGet(httpClient, treeURL)
	if err != nil {
		return nil, fmt.Errorf("failed to reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned status %d listing the repository tree", resp.StatusCode)
	}

	var tree githubTreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub tree response")
	}

	dirs := selectSkillDirsFromTree(tree.Tree, spec.skillDir)
	truncated := tree.Truncated || len(dirs) > maxDiscoverCandidates
	if len(dirs) > maxDiscoverCandidates {
		dirs = dirs[:maxDiscoverCandidates]
	}

	rawPrefix := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s",
		spec.owner, spec.repo, escapeRefPath(spec.ref))

	candidates := make([]SkillCandidate, 0, len(dirs))
	for _, dir := range dirs {
		mdPath := "SKILL.md"
		if dir != "" {
			mdPath = dir + "/SKILL.md"
		}
		body, err := fetchRawFile(httpClient, buildRawGitHubURL(rawPrefix, mdPath))
		if err != nil {
			// unreadable/oversize SKILL.md → skip this candidate
			continue
		}
		name, description := skillpkg.ParseSkillFrontmatter(string(body))
		if name == "" {
			if dir == "" {
				name = spec.repo
			} else {
				name = path.Base(dir)
			}
		}
		importURL := fmt.Sprintf("https://github.com/%s/%s/tree/%s",
			spec.owner, spec.repo, spec.ref)
		if dir != "" {
			importURL += "/" + dir
		}
		display := dir
		if display == "" {
			display = "(root)"
		}
		candidates = append(candidates, SkillCandidate{
			Name:        name,
			Description: description,
			Path:        display,
			ImportURL:   importURL,
		})
	}

	return &SkillDiscoveryResponse{Candidates: candidates, Truncated: truncated}, nil
}

// DiscoverSkills lists the skills found under a GitHub repo/folder URL without
// importing them. GitHub-only; other sources return 400. Workspace membership
// is enforced by the /api/skills route group middleware.
func (h *Handler) DiscoverSkills(w http.ResponseWriter, r *http.Request) {
	var req ImportSkillRequest // reuse the { "url": ... } body shape
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	source, normalized, err := detectImportSource(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if source != sourceGitHub {
		writeError(w, http.StatusBadRequest, "only GitHub repositories are supported for bulk discovery")
		return
	}

	result, err := h.discoverGitHubSkills(normalized)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
