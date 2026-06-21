-- Reverse FIR-1771 Test as user seeding.
DELETE FROM cerebro_tool_policy WHERE tool_key = 'tools:test-as-user';
DELETE FROM cerebro_capability WHERE capability_key = 'tools:test-as-user';
