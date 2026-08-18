-- An Extension release is complete once its versioned Squad exists.  Its
-- internal Agents may remain unbound until a compatible local runtime is
-- connected, so runtime_id is deliberately optional after completion.
ALTER TABLE platform_extension_release
    DROP CONSTRAINT platform_extension_release_runtime_routing_check;

ALTER TABLE platform_extension_release
    ADD CONSTRAINT platform_extension_release_runtime_routing_check
    CHECK (
        (runtime_id IS NULL AND squad_id IS NULL)
        OR (
            runtime_binding_mode = 'fixed'
            AND squad_id IS NOT NULL
        )
        OR (
            runtime_binding_mode = 'pool'
            AND squad_id IS NOT NULL
            AND runtime_id IS NULL
        )
    );
