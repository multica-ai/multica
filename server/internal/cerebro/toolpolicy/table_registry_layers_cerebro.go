package toolpolicy

// CEREBRO-FEATURE(FIR-2269): author firtal_registry per-data-source rules at
// every actor layer, not only the per-agent layer.
//
// Every actor scope reads authored chain rows (cerebro_tool_policy,
// resource_pattern = data source id), exactly as chainGateDataSource enforces,
// and renders one authorable row per data source. It mirrors appendRepoRows/loadRepoPolicy
// Settings: a catalog (injected, because the FDR proxy lives in package handler)
// crossed with the authored per-layer settings, resolved against Base.

import "context"

// AppendRegistryDataSourceRows folds the workspace's data-source catalog into the
// table as one authorable firtal_registry per-source row each, populated with the
// authored chain settings for the query's scope and resolved against Base. It is
// used at workspace, runtime, agent, group, user, and system views. dataSources
// is the catalog supplied by the handler (the FDR
// proxy is owned there, so toolpolicy.Table stays free of the registry API). A
// data source whose row was already authored on the table is left as-is.
func (s *Store) AppendRegistryDataSourceRows(ctx context.Context, in TableQuery, dataSources []RegistryDataSource, out []TableRow) ([]TableRow, error) {
	if len(dataSources) == 0 {
		return out, nil
	}
	groupIDs, err := s.resolveGroupIDs(ctx, in.WorkspaceID, in.UserID, in.GroupIDs)
	if err != nil {
		return nil, err
	}
	settings, err := s.loadResourcePolicySettings(ctx, in, groupIDs, resourcePolicyFilter{
		toolKeys: []string{RegistryToolKey},
		scope:    resourcePatternNonEmpty,
	})
	if err != nil {
		return nil, err
	}

	// The Effective column must show what the registry gate enforces: openable
	// when the workspace member-override flag is on, tighten-only otherwise
	// (FIR-2351). These methods are called by handlers directly (not through
	// Table), so decide the mode here.
	if in.mode == "" {
		in.mode = ModeHardFloor
		if s.MemberOverrideEnabled(ctx, in.WorkspaceID) {
			in.mode = ModeOpenable
		}
	}

	authored := map[string]bool{}
	for _, row := range out {
		if row.ToolKey == RegistryToolKey && row.ResourcePattern != "" {
			authored[row.ResourcePattern] = true
		}
	}

	for _, ds := range dataSources {
		id := ds.ID
		if id == "" || authored[id] {
			continue
		}
		row := TableRow{
			ToolKey:         RegistryToolKey,
			ResourcePattern: id,
			Title:           ds.Name,
			Category:        registryDataSourceCategory,
			Source:          registryDataSourceSource,
			Layers:          map[Layer]Setting{},
			Conditions:      map[Layer]*Condition{},
		}
		if cell, ok := settings[resourcePolicyKey{toolKey: RegistryToolKey, resourcePattern: id}]; ok {
			for l, set := range cell.layers {
				row.Layers[l] = set
			}
			for l, cond := range cell.conditions {
				row.Conditions[l] = cond
			}
			if len(cell.groups) > 0 {
				row.Layers[LayerGroup] = CombineGroups(cell.groups...)
			}
		}
		row.Effective, err = s.resolveRegistryDataSourceEffective(ctx, in, row.ResourcePattern)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// AppendAuthoredRegistryDataSourceRows appends only already-authored
// firtal_registry per-data-source rows. It is retained for callers that need a
// sparse catalog projection rather than the full authoring table.
func (s *Store) AppendAuthoredRegistryDataSourceRows(ctx context.Context, in TableQuery, dataSources []RegistryDataSource, out []TableRow) ([]TableRow, error) {
	if len(dataSources) == 0 {
		return out, nil
	}
	groupIDs, err := s.resolveGroupIDs(ctx, in.WorkspaceID, in.UserID, in.GroupIDs)
	if err != nil {
		return nil, err
	}
	settings, err := s.loadResourcePolicySettings(ctx, in, groupIDs, resourcePolicyFilter{
		toolKeys: []string{RegistryToolKey},
		scope:    resourcePatternNonEmpty,
	})
	if err != nil {
		return nil, err
	}

	// The Effective column must show what the registry gate enforces: openable
	// when the workspace member-override flag is on, tighten-only otherwise
	// (FIR-2351). These methods are called by handlers directly (not through
	// Table), so decide the mode here.
	if in.mode == "" {
		in.mode = ModeHardFloor
		if s.MemberOverrideEnabled(ctx, in.WorkspaceID) {
			in.mode = ModeOpenable
		}
	}

	authored := map[string]bool{}
	for _, row := range out {
		if row.ToolKey == RegistryToolKey && row.ResourcePattern != "" {
			authored[row.ResourcePattern] = true
		}
	}

	for _, ds := range dataSources {
		id := ds.ID
		cell, ok := settings[resourcePolicyKey{toolKey: RegistryToolKey, resourcePattern: id}]
		if id == "" || !ok || authored[id] {
			continue
		}
		row := TableRow{
			ToolKey:         RegistryToolKey,
			ResourcePattern: id,
			Title:           ds.Name,
			Category:        registryDataSourceCategory,
			Source:          registryDataSourceSource,
			Layers:          map[Layer]Setting{},
			Conditions:      map[Layer]*Condition{},
		}
		for l, set := range cell.layers {
			row.Layers[l] = set
		}
		for l, cond := range cell.conditions {
			row.Conditions[l] = cond
		}
		if len(cell.groups) > 0 {
			row.Layers[LayerGroup] = CombineGroups(cell.groups...)
		}
		row.Effective, err = s.resolveRegistryDataSourceEffective(ctx, in, row.ResourcePattern)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// resolveRegistryDataSourceEffective mirrors the live registry gate: one
// concrete call must pass both its exact data-source row and the
// capability-wide scoping row. The same RequestContext (including the concrete
// data_source_id) is used for both decisions.
func (s *Store) resolveRegistryDataSourceEffective(ctx context.Context, in TableQuery, dataSourceID string) (Effective, error) {
	reqCtx := in.RequestContext
	argValues := make(map[string]string, len(reqCtx.ArgValues)+1)
	for key, value := range reqCtx.ArgValues {
		argValues[key] = value
	}
	reqCtx.ArgValues = argValues
	if reqCtx.ArgValues["data_source_id"] == "" {
		reqCtx.ArgValues["data_source_id"] = dataSourceID
	}
	query := Query{
		WorkspaceID:    in.WorkspaceID,
		ToolKey:        RegistryToolKey,
		RuntimeID:      in.RuntimeID,
		AgentID:        in.AgentID,
		UserID:         in.UserID,
		GroupIDs:       in.GroupIDs,
		OnBehalfOfID:   in.OnBehalfOfID,
		SystemID:       in.SystemID,
		Base:           in.Base,
		IsSystem:       in.IsSystem,
		RequestContext: reqCtx,
		Eval:           in.Eval,
	}
	query.ResourcePattern = dataSourceID
	exact, err := s.ResolveDeclared(ctx, query)
	if err != nil {
		return Effective{}, err
	}
	query.ResourcePattern = ""
	wide, err := s.ResolveDeclared(ctx, query)
	if err != nil {
		return Effective{}, err
	}
	if rank(wide.Setting) > rank(exact.Setting) {
		return wide, nil
	}
	return exact, nil
}
