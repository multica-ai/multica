drop policy if exists "authenticated can read meta proposal audit" on public.meta_proposal_audit_log;
drop policy if exists "authenticated can review meta proposals" on public.meta_proposals;
drop policy if exists "authenticated can read meta proposals" on public.meta_proposals;

revoke select, insert on table public.meta_proposal_audit_log from service_role;
revoke select, insert, update on table public.meta_proposals from service_role;
revoke select on table public.meta_proposal_audit_log from authenticated;
revoke update (approval_status) on table public.meta_proposals from authenticated;
revoke select on table public.meta_proposals from authenticated;
revoke usage on schema auth from authenticated, service_role;

drop trigger if exists log_meta_proposal_audit_update on public.meta_proposals;
drop trigger if exists log_meta_proposal_audit_insert on public.meta_proposals;
drop trigger if exists set_meta_proposal_review_fields on public.meta_proposals;

drop function if exists private.log_meta_proposal_audit();
drop function if exists public.set_meta_proposal_review_fields();

drop table if exists public.meta_proposal_audit_log;
drop table if exists public.meta_proposals;
drop type if exists public.meta_proposal_approval_status;
