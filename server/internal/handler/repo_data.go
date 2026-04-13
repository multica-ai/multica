package handler

// repoDataFromJSON decodes the workspace.repos JSONB blob into the shape sent
// to daemons. It's tolerant of v1 rows (pre-migration 040) — those become
// type=github entries with the url carried forward.
func repoDataFromJSON(raw []byte) []RepoData {
	repos, err := parseWorkspaceRepos(raw)
	if err != nil || len(repos) == 0 {
		return []RepoData{}
	}
	out := make([]RepoData, len(repos))
	for i, r := range repos {
		out[i] = RepoData{
			ID:          r.ID,
			Name:        r.Name,
			Type:        r.Type,
			URL:         r.URL,
			LocalPath:   r.LocalPath,
			Description: r.Description,
		}
	}
	return out
}

// filterReposByIDs keeps only the repos whose id is in the given set, preserving
// the input order of ids so callers can imply priority by order in the returned
// slice (e.g. the project's first repo becomes the default start directory).
func filterReposByIDs(repos []RepoData, ids []string) []RepoData {
	if len(ids) == 0 {
		return nil
	}
	byID := make(map[string]RepoData, len(repos))
	for _, r := range repos {
		byID[r.ID] = r
	}
	out := make([]RepoData, 0, len(ids))
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			out = append(out, r)
		}
	}
	return out
}
