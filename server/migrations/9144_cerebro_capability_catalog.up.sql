-- 9144_cerebro_capability_catalog: durable model for the read-only capability dictionary.
--
-- The schema mirrors server/internal/cerebro/capabilitycatalog exactly: one
-- official capability ID plus provider/surface-qualified key forms. It is
-- intentionally disconnected from cerebro_tool_policy and every runtime gate.

CREATE TABLE IF NOT EXISTS cerebro_canonical_capability (
    canonical_id TEXT PRIMARY KEY,
    family TEXT NOT NULL,
    source_reference TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_canonical_capability_id_not_blank
        CHECK (length(trim(canonical_id)) > 0),
    CONSTRAINT cerebro_canonical_capability_source_not_blank
        CHECK (length(trim(source_reference)) > 0),
    CONSTRAINT cerebro_canonical_capability_family_known CHECK (family IN (
        'platform', 'gateway', 'connection', 'mcp', 'api-connection', 'runtime', 'tool'
    ))
);

CREATE INDEX IF NOT EXISTS idx_cerebro_canonical_capability_family
    ON cerebro_canonical_capability (family, canonical_id);

CREATE TABLE IF NOT EXISTS cerebro_capability_alias (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    capability_id TEXT NOT NULL
        REFERENCES cerebro_canonical_capability(canonical_id) ON DELETE CASCADE,
    surface TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT '',
    key_value TEXT NOT NULL,
    resource_pattern TEXT NOT NULL DEFAULT '',
    key_source TEXT NOT NULL DEFAULT '',
    relation TEXT NOT NULL,
    source_reference TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_capability_alias_surface_known CHECK (surface IN (
        'policy', 'gateway', 'bridge', 'mcp', 'api-endpoint', 'runtime'
    )),
    CONSTRAINT cerebro_capability_alias_relation_known CHECK (relation IN (
        'canonical', 'alias', 'variant'
    )),
    CONSTRAINT cerebro_capability_alias_value_not_blank
        CHECK (length(trim(key_value)) > 0),
    CONSTRAINT cerebro_capability_alias_source_not_blank
        CHECK (length(trim(source_reference)) > 0),
    CONSTRAINT cerebro_capability_alias_unambiguous UNIQUE (
        surface, provider, key_value, resource_pattern, key_source
    )
);

CREATE INDEX IF NOT EXISTS idx_cerebro_capability_alias_capability
    ON cerebro_capability_alias (capability_id, relation, surface, key_value);

-- The proven cross-runtime identity from the architecture: Gateway web_fetch
-- and Claude WebFetch are aliases; Gemini remains a variant until parity is
-- proven. The canonical ID and relations match capabilitycatalog.WebFetch().
INSERT INTO cerebro_canonical_capability
    (canonical_id, family, source_reference)
VALUES
    ('tool:web_fetch', 'tool', 'server/internal/cerebro/capabilitycatalog/catalog.go')
ON CONFLICT (canonical_id) DO NOTHING;

INSERT INTO cerebro_capability_alias
    (capability_id, surface, provider, key_value, resource_pattern, key_source, relation, source_reference)
VALUES
    ('tool:web_fetch', 'gateway', '', 'web_fetch', '', '', 'canonical',
        'server/internal/cerebro/runtime/firtal_gateway_tools_extended.go'),
    ('tool:web_fetch', 'policy', '', 'web_fetch', '', 'gateway', 'alias',
        'server/internal/cerebro/toolpolicy/table.go'),
    ('tool:web_fetch', 'runtime', 'claude', 'WebFetch', '', '', 'alias',
        'server/internal/cerebro/claudehook/claudehook.go'),
    ('tool:web_fetch', 'policy', '', 'tools:WebFetch', '', 'runtime_report', 'alias',
        'server/internal/cerebro/claudehook/claudehook.go'),
    ('tool:web_fetch', 'runtime', 'gemini', 'web_fetch', '', '', 'variant',
        'server/internal/cerebro/capabilities/discovery.go'),
    ('tool:web_fetch', 'policy', '', 'tools:web_fetch', '', 'runtime_report', 'variant',
        'server/internal/cerebro/capabilities/discovery.go')
ON CONFLICT (surface, provider, key_value, resource_pattern, key_source) DO NOTHING;

-- Backfill connection-wide canonical capabilities already discovered in each
-- workspace. Identical names converge because the canonical identity is global.
INSERT INTO cerebro_canonical_capability
    (canonical_id, family, source_reference)
SELECT DISTINCT
       'connection:' || lower(trim(wc.name)),
       'connection',
       'server/internal/cerebro/connections/capability.go'
FROM workspace_connection wc
WHERE length(trim(wc.name)) > 0
ON CONFLICT (canonical_id) DO NOTHING;

INSERT INTO cerebro_capability_alias
    (capability_id, surface, provider, key_value, resource_pattern, key_source, relation, source_reference)
SELECT DISTINCT
       'connection:' || lower(trim(wc.name)),
       'policy', '', 'connection:' || wc.name, '', 'connection', 'canonical',
       'server/internal/cerebro/connections/capability.go'
FROM workspace_connection wc
WHERE length(trim(wc.name)) > 0
ON CONFLICT (surface, provider, key_value, resource_pattern, key_source) DO NOTHING;

-- Persist every discovered MCP connection tool in both callable and policy
-- forms. The unique key-form constraint rejects alias collisions globally.
WITH discovered AS (
    SELECT DISTINCT wc.name AS connection_name, tool->>'name' AS tool_name
    FROM workspace_connection wc
    CROSS JOIN LATERAL jsonb_array_elements(wc.tools) tool
    WHERE wc.type = 'mcp_http' AND length(trim(tool->>'name')) > 0
)
INSERT INTO cerebro_canonical_capability
    (canonical_id, family, source_reference)
SELECT 'connection:' || lower(trim(connection_name)) || ':mcp:' || lower(trim(tool_name)),
       'mcp', 'server/internal/cerebro/capabilitycatalog/catalog.go'
FROM discovered
ON CONFLICT (canonical_id) DO NOTHING;

WITH discovered AS (
    SELECT DISTINCT wc.name AS connection_name, tool->>'name' AS tool_name
    FROM workspace_connection wc
    CROSS JOIN LATERAL jsonb_array_elements(wc.tools) tool
    WHERE wc.type = 'mcp_http' AND length(trim(tool->>'name')) > 0
), forms AS (
    SELECT 'connection:' || lower(trim(connection_name)) || ':mcp:' || lower(trim(tool_name)) AS capability_id,
           connection_name, tool_name
    FROM discovered
)
INSERT INTO cerebro_capability_alias
    (capability_id, surface, provider, key_value, resource_pattern, key_source, relation, source_reference)
SELECT capability_id, 'mcp', '', 'mcp__' || connection_name || '__' || tool_name,
       '', '', 'canonical', 'server/internal/cerebro/toolpolicy/table_connection.go'
FROM forms
UNION ALL
SELECT capability_id, 'policy', '', 'connection:' || connection_name,
       tool_name, 'connection-tool', 'alias',
       'server/internal/cerebro/toolpolicy/table_connection.go'
FROM forms
ON CONFLICT (surface, provider, key_value, resource_pattern, key_source) DO NOTHING;

-- Persist API connection endpoint identities. The callable name uses the same
-- normalization contract as runtime.apiToolName.
WITH endpoints AS (
    SELECT DISTINCT wc.name AS connection_name, upper(method) AS method, ep->>'path' AS path
    FROM workspace_connection wc
    CROSS JOIN LATERAL jsonb_array_elements(wc.endpoint_permissions) ep
    CROSS JOIN LATERAL jsonb_array_elements_text(ep->'methods') method
    WHERE wc.type = 'api' AND length(trim(ep->>'path')) > 0
), named AS (
    SELECT connection_name, method, path,
           'connection:' || lower(trim(connection_name)) || ':api:' || method || ':' || path AS capability_id,
           trim(both '_' FROM regexp_replace(lower(connection_name), '[^a-z0-9]+', '_', 'g'))
             || '__' ||
           trim(both '_' FROM regexp_replace(lower(method || ' ' || path), '[^a-z0-9]+', '_', 'g'))
             AS exposed_name
    FROM endpoints
)
INSERT INTO cerebro_canonical_capability
    (canonical_id, family, source_reference)
SELECT capability_id, 'api-connection',
       'server/internal/cerebro/runtime/api_connection_tools.go'
FROM named
ON CONFLICT (canonical_id) DO NOTHING;

WITH endpoints AS (
    SELECT DISTINCT wc.name AS connection_name, upper(method) AS method, ep->>'path' AS path
    FROM workspace_connection wc
    CROSS JOIN LATERAL jsonb_array_elements(wc.endpoint_permissions) ep
    CROSS JOIN LATERAL jsonb_array_elements_text(ep->'methods') method
    WHERE wc.type = 'api' AND length(trim(ep->>'path')) > 0
), named AS (
    SELECT connection_name, method, path,
           'connection:' || lower(trim(connection_name)) || ':api:' || method || ':' || path AS capability_id,
           trim(both '_' FROM regexp_replace(lower(connection_name), '[^a-z0-9]+', '_', 'g'))
             || '__' ||
           trim(both '_' FROM regexp_replace(lower(method || ' ' || path), '[^a-z0-9]+', '_', 'g'))
             AS exposed_name
    FROM endpoints
)
INSERT INTO cerebro_capability_alias
    (capability_id, surface, provider, key_value, resource_pattern, key_source, relation, source_reference)
SELECT capability_id, 'api-endpoint', '', exposed_name, '', '', 'canonical',
       'server/internal/cerebro/runtime/api_connection_tools.go'
FROM named
WHERE length(exposed_name) > 0
UNION ALL
SELECT capability_id, 'policy', '', 'connection:' || connection_name,
       method || ' ' || path, 'connection-endpoint', 'alias',
       'server/internal/cerebro/toolpolicy/table_connection.go'
FROM named
ON CONFLICT (surface, provider, key_value, resource_pattern, key_source) DO NOTHING;
