# Issue Comment Suggested Follow-ups Implementation Plan

1. Generalize the suggestion value and sanitizer so Chat and Issue comments use
   one contract without changing Chat API behavior.
2. Add the server-owned comment JSONB column in an isolated migration and
   regenerate sqlc output.
3. Extend comment queries, response schemas, TypeScript types, and realtime
   cache updates with defensive parsing defaults.
4. Add an asynchronous issue-comment suggestion pass after a successful task
   comment is finalized, using bounded context and the existing LLM client.
5. Add a run endpoint that resolves the stored action and trusted source task,
   checks freshness and permission, posts a normal reply, and reuses existing
   trigger handling.
6. Add shared Web/Desktop pills below eligible issue comments and map dispatch
   outcomes to accurate feedback.
7. Add backend and frontend regression tests, then run sqlc, formatting,
   typecheck, focused tests, full tests, and semantic end-to-end verification.
8. Review the diff against repository boundaries and contribution requirements,
   commit the implementation, push the feature branch, and open a linked PR with
   screenshots and verification evidence.

