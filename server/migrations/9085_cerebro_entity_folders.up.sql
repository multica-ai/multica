-- FIR-1412: Folders (with sub-folders) for the Skills and Autopilots
-- interfaces. One recursive folder tree per workspace per surface, plus a
-- membership table that files an individual skill/autopilot into a folder.
--
-- Design: a single cerebro-namespaced table serves both surfaces, scoped by
-- `kind` ('skill' | 'autopilot'). This keeps the feature fully inside the
-- cerebro zone — the upstream `skill` and `autopilot` tables are NOT touched.
-- The frontend fetches folders + memberships separately and groups the
-- existing skill/autopilot lists client-side.

CREATE TABLE IF NOT EXISTS cerebro_entity_folder (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    -- parent_id: null = root of the workspace tree for this kind. Recursive
    -- via the self-reference; deleting a folder cascades to its sub-folders.
    parent_id    UUID REFERENCES cerebro_entity_folder(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('skill', 'autopilot')),
    name         TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Two surfaces (or two levels) may legitimately reuse a folder name, so
    -- kind + parent are part of the uniqueness key.
    UNIQUE (workspace_id, kind, parent_id, name)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_entity_folder_ws_kind
    ON cerebro_entity_folder(workspace_id, kind);
CREATE INDEX IF NOT EXISTS idx_cerebro_entity_folder_parent
    ON cerebro_entity_folder(parent_id) WHERE parent_id IS NOT NULL;

-- Membership: which folder a single skill/autopilot lives in. An item is in at
-- most one folder (PRIMARY KEY on kind + item_id). No FK on item_id because it
-- is polymorphic (skill OR autopilot); deleting a folder cascades the
-- membership row away, so the item falls back to root. Orphaned rows (item
-- itself deleted) are harmless — the frontend only groups items that exist.
CREATE TABLE IF NOT EXISTS cerebro_entity_folder_item (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('skill', 'autopilot')),
    item_id      UUID NOT NULL,
    folder_id    UUID NOT NULL REFERENCES cerebro_entity_folder(id) ON DELETE CASCADE,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, item_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_entity_folder_item_folder
    ON cerebro_entity_folder_item(folder_id);
CREATE INDEX IF NOT EXISTS idx_cerebro_entity_folder_item_ws_kind
    ON cerebro_entity_folder_item(workspace_id, kind);
