-- Product map MVP (SY-20): workspace-scoped product tree with evidence-backed
-- live status.
--
-- product_nodes: the product tree. Each node is workspace-scoped and may be a
-- product (parent_id NULL) or a functional module / page under a product.
-- status_source is the per-product configurable evidence source the owner
-- confirmed (2026-08-11): 'pmo' = PMO 上线状态 is authoritative, 'code_repo' =
-- the product's own code repository (default-branch merge/release) is
-- authoritative. status must never be inferred from Issue `done` alone.
--
-- product_refs: polymorphic traceability links from a product node to a
-- Multica project or issue (ref_type IN ('project','issue')).
--
-- product_editors: product-level editor ACL (user_id is a "user".id). MVP
-- registers the first editors but does not open the full manual-edit workflow.
CREATE TABLE product_nodes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    parent_id UUID REFERENCES product_nodes(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN (
        'in_development',
        'pending_release',
        'pending_confirmation',
        'released'
    )),
    status_source TEXT NOT NULL DEFAULT 'pmo' CHECK (status_source IN (
        'pmo',
        'code_repo'
    )),
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX product_nodes_workspace_slug_idx ON product_nodes (workspace_id, slug);
CREATE INDEX product_nodes_workspace_parent_idx ON product_nodes (workspace_id, parent_id, sort_order);

CREATE TABLE product_refs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID NOT NULL REFERENCES product_nodes(id) ON DELETE CASCADE,
    ref_type TEXT NOT NULL CHECK (ref_type IN ('project', 'issue')),
    ref_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (product_id, ref_type, ref_id)
);

CREATE INDEX product_refs_product_idx ON product_refs (product_id);
CREATE INDEX product_refs_target_idx ON product_refs (ref_type, ref_id);

CREATE TABLE product_editors (
    product_id UUID NOT NULL REFERENCES product_nodes(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (product_id, user_id)
);
