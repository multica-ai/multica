-- FIR-1587: allow an agent actor as the proposer of a skill change request.
--
-- Migration 9013 declared `proposed_by UUID NOT NULL REFERENCES "user"(id)`.
-- The FIR-1587 fix (CEREBRO-PATCH skill-change-request-actor) attributes a
-- proposal to the acting *agent* via resolveActor so the human owner is no
-- longer the excluded proposer and gets the inbox notification. But an agent
-- id is not present in "user", so the insert violated this FK and crashed with
-- a 500 ("skill_change_request_proposed_by_fkey"). proposed_by is resolved
-- polymorphically when rendering the inbox body (user first, then agent), and
-- no query joins proposed_by back to "user", so the strict FK is unnecessary.
-- Drop it — same polymorphic-actor pattern the comment author columns use.
ALTER TABLE skill_change_request
    DROP CONSTRAINT IF EXISTS skill_change_request_proposed_by_fkey;
