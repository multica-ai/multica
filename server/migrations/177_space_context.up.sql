-- Space-level operating context sits between Workspace policy and concrete
-- Project / Issue context. It is injected only into tasks bound to this Space;
-- context-free tasks such as Direct Chat never receive it.
ALTER TABLE workspace_space
    ADD COLUMN context TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN workspace_space.context IS
    'Operating context injected into Agent runs bound to this Space. It does not grant access or widen the task token scope.';
