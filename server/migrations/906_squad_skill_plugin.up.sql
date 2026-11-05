-- Skills and plugins applied to a squad. Every member of the squad gets the
-- squad's skills on every task; plugins are requirements the squad leader
-- checks for and installs on the runtime of each project it works on.
--
-- Relationships are application-owned by repository policy: there are no
-- foreign keys or cascades. Squad and skill writes validate parents in the
-- handler, and workspace teardown deletes these rows explicitly.
CREATE TABLE squad_skill (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    squad_id UUID NOT NULL,
    skill_id UUID NOT NULL,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE squad_plugin (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    squad_id UUID NOT NULL,
    plugin_name TEXT NOT NULL CHECK (char_length(plugin_name) BETWEEN 1 AND 128),
    install_hint TEXT NOT NULL DEFAULT '' CHECK (char_length(install_hint) <= 1024),
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
