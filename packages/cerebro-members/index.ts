// JEH-1067 (Bundle B) — Cerebro members overlay package.
//
// Hosts the Groups-column + group-filter slot wired into upstream MembersTab,
// plus the new Member detail page (groups membership, effective access,
// own runtimes/agents, overrides placeholder for Bundle C).
//
// Mirrors `packages/cerebro-groups` layout: headless surface (`./`) re-exports
// types/queries only; UI components live behind `./views` so non-UI consumers
// (cerebro-realtime, server integrations) can import the headless API without
// pulling React into unrelated test resolution graphs.
export {};
