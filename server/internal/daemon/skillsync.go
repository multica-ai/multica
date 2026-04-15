package daemon

import (
	"context"
	"fmt"
)

const daemonSkillSyncManagedBy = "daemon-skill-sync"

// WorkspaceSkillService is the minimal workspace skill CRUD surface needed by reconcile.
type WorkspaceSkillService interface {
	ListWorkspaceSkills(ctx context.Context, workspaceID string) ([]WorkspaceSkill, error)
	CreateWorkspaceSkill(ctx context.Context, workspaceID string, req CreateWorkspaceSkillRequest) (*WorkspaceSkill, error)
	UpdateWorkspaceSkill(ctx context.Context, workspaceID, skillID string, req UpdateWorkspaceSkillRequest) (*WorkspaceSkill, error)
	DeleteWorkspaceSkill(ctx context.Context, workspaceID, skillID string) error
}

// WorkspaceSkillSyncRequest describes one reconcile pass.
type WorkspaceSkillSyncRequest struct {
	WorkspaceID   string
	SyncDir       string
	DaemonID      string
	Profile       string
	DeleteManaged bool
	LocalSkills   []ScannedSkill
}

// WorkspaceSkillSyncResult summarizes reconcile decisions.
type WorkspaceSkillSyncResult struct {
	Created   []string
	Updated   []string
	Deleted   []string
	Unchanged []string
	Conflicts []string
}

// WorkspaceSkillSyncConfig is the persisted config written onto daemon-managed skills.
type WorkspaceSkillSyncConfig struct {
	ManagedBy          string                          `json:"managed_by"`
	WorkspaceSkillSync WorkspaceSkillSyncConfigDetails `json:"workspace_skill_sync"`
}

// WorkspaceSkillSyncConfigDetails identifies the daemon sync source and manifest.
type WorkspaceSkillSyncConfigDetails struct {
	Dir      string `json:"dir"`
	Hash     string `json:"hash"`
	DaemonID string `json:"daemon_id"`
	Profile  string `json:"profile"`
}

// ReconcileWorkspaceSkills syncs local scanned skills into one workspace.
func ReconcileWorkspaceSkills(ctx context.Context, service WorkspaceSkillService, req WorkspaceSkillSyncRequest) (*WorkspaceSkillSyncResult, error) {
	remoteSkills, err := service.ListWorkspaceSkills(ctx, req.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace skills: %w", err)
	}

	result := &WorkspaceSkillSyncResult{}
	localByName := make(map[string]ScannedSkill, len(req.LocalSkills))
	remoteByName := make(map[string]WorkspaceSkill, len(remoteSkills))
	for _, skill := range req.LocalSkills {
		localByName[skill.Name] = skill
	}
	for _, skill := range remoteSkills {
		remoteByName[skill.Name] = skill
	}

	for _, localSkill := range req.LocalSkills {
		remoteSkill, exists := remoteByName[localSkill.Name]
		if !exists {
			if _, err := service.CreateWorkspaceSkill(ctx, req.WorkspaceID, buildCreateWorkspaceSkillRequest(localSkill, req)); err != nil {
				return nil, fmt.Errorf("create workspace skill %q: %w", localSkill.Name, err)
			}
			result.Created = append(result.Created, localSkill.Name)
			continue
		}

		meta, managed := parseDaemonSkillSyncConfig(remoteSkill.Config)
		if !managed {
			result.Conflicts = append(result.Conflicts, localSkill.Name)
			continue
		}
		if !sameDaemonSkillSource(meta, req.SyncDir, req.Profile) {
			result.Conflicts = append(result.Conflicts, localSkill.Name)
			continue
		}
		if meta.WorkspaceSkillSync.Hash == localSkill.Hash {
			result.Unchanged = append(result.Unchanged, localSkill.Name)
			continue
		}

		if _, err := service.UpdateWorkspaceSkill(ctx, req.WorkspaceID, remoteSkill.ID, buildUpdateWorkspaceSkillRequest(localSkill, req)); err != nil {
			return nil, fmt.Errorf("update workspace skill %q: %w", localSkill.Name, err)
		}
		result.Updated = append(result.Updated, localSkill.Name)
	}

	for _, remoteSkill := range remoteSkills {
		if _, exists := localByName[remoteSkill.Name]; exists {
			continue
		}
		meta, managed := parseDaemonSkillSyncConfig(remoteSkill.Config)
		if !managed || !sameDaemonSkillSource(meta, req.SyncDir, req.Profile) {
			continue
		}
		if !req.DeleteManaged {
			result.Unchanged = append(result.Unchanged, remoteSkill.Name)
			continue
		}
		if err := service.DeleteWorkspaceSkill(ctx, req.WorkspaceID, remoteSkill.ID); err != nil {
			return nil, fmt.Errorf("delete workspace skill %q: %w", remoteSkill.Name, err)
		}
		result.Deleted = append(result.Deleted, remoteSkill.Name)
	}

	return result, nil
}

func buildCreateWorkspaceSkillRequest(skill ScannedSkill, req WorkspaceSkillSyncRequest) CreateWorkspaceSkillRequest {
	return CreateWorkspaceSkillRequest{
		Name:    skill.Name,
		Content: skill.Content,
		Config:  buildDaemonSkillSyncConfig(req, skill.Hash),
		Files:   buildWorkspaceSkillFiles(skill.Files),
	}
}

func buildUpdateWorkspaceSkillRequest(skill ScannedSkill, req WorkspaceSkillSyncRequest) UpdateWorkspaceSkillRequest {
	content := skill.Content
	return UpdateWorkspaceSkillRequest{
		Content: &content,
		Config:  buildDaemonSkillSyncConfig(req, skill.Hash),
		Files:   buildWorkspaceSkillFiles(skill.Files),
	}
}

func buildWorkspaceSkillFiles(files []ScannedSkillFile) []WorkspaceSkillFile {
	if len(files) == 0 {
		return []WorkspaceSkillFile{}
	}
	result := make([]WorkspaceSkillFile, 0, len(files))
	for _, file := range files {
		result = append(result, WorkspaceSkillFile{
			Path:    file.Path,
			Content: file.Content,
		})
	}
	return result
}

func buildDaemonSkillSyncConfig(req WorkspaceSkillSyncRequest, hash string) WorkspaceSkillSyncConfig {
	return WorkspaceSkillSyncConfig{
		ManagedBy: daemonSkillSyncManagedBy,
		WorkspaceSkillSync: WorkspaceSkillSyncConfigDetails{
			Dir:      req.SyncDir,
			Hash:     hash,
			DaemonID: req.DaemonID,
			Profile:  req.Profile,
		},
	}
}

func parseDaemonSkillSyncConfig(config any) (WorkspaceSkillSyncConfig, bool) {
	root, ok := config.(map[string]any)
	if !ok {
		return WorkspaceSkillSyncConfig{}, false
	}

	managedBy, ok := root["managed_by"].(string)
	if !ok || managedBy != daemonSkillSyncManagedBy {
		return WorkspaceSkillSyncConfig{}, false
	}

	syncRaw, ok := root["workspace_skill_sync"].(map[string]any)
	if !ok {
		return WorkspaceSkillSyncConfig{}, false
	}

	return WorkspaceSkillSyncConfig{
		ManagedBy: managedBy,
		WorkspaceSkillSync: WorkspaceSkillSyncConfigDetails{
			Dir:      stringValue(syncRaw["dir"]),
			Hash:     stringValue(syncRaw["hash"]),
			DaemonID: stringValue(syncRaw["daemon_id"]),
			Profile:  stringValue(syncRaw["profile"]),
		},
	}, true
}

func sameDaemonSkillSource(config WorkspaceSkillSyncConfig, syncDir, profile string) bool {
	if config.ManagedBy != daemonSkillSyncManagedBy {
		return false
	}
	if config.WorkspaceSkillSync.Dir != syncDir {
		return false
	}
	if config.WorkspaceSkillSync.Profile != profile {
		return false
	}
	return true
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}
