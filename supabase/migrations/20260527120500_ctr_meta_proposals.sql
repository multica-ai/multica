create extension if not exists pgcrypto;
create schema if not exists private;

do $$
begin
  create type public.meta_proposal_approval_status as enum ('pending', 'approved', 'rejected');
exception
  when duplicate_object then null;
end $$;

create table if not exists public.meta_proposals (
  id uuid primary key default gen_random_uuid(),
  url text not null,
  impact_score numeric(12, 4) not null default 0,
  hypothesis text not null,
  current_meta_title text,
  current_meta_description text,
  proposed_meta_title text not null,
  proposed_meta_description text not null,
  rationale text not null,
  confidence_score numeric(5, 4) not null,
  gsc_impressions bigint not null default 0,
  gsc_clicks bigint not null default 0,
  gsc_ctr numeric(8, 6) not null default 0,
  gsc_position numeric(8, 3),
  approval_status public.meta_proposal_approval_status not null default 'pending',
  reviewed_at timestamptz,
  reviewed_by uuid references auth.users(id) on delete set null,
  source text not null default 'analysis_agent',
  analysis_run_id text,
  metadata jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint meta_proposals_url_not_blank check (length(btrim(url)) > 0),
  constraint meta_proposals_confidence_score_range check (confidence_score >= 0 and confidence_score <= 1),
  constraint meta_proposals_gsc_non_negative check (
    gsc_impressions >= 0
    and gsc_clicks >= 0
    and gsc_ctr >= 0
    and (gsc_position is null or gsc_position >= 0)
  ),
  constraint meta_proposals_review_fields_match_status check (
    (approval_status = 'pending' and reviewed_at is null and reviewed_by is null)
    or (approval_status in ('approved', 'rejected') and reviewed_at is not null)
  )
);

create index if not exists idx_meta_proposals_approval_status_created
  on public.meta_proposals (approval_status, created_at desc);

create index if not exists idx_meta_proposals_url
  on public.meta_proposals (url);

create index if not exists idx_meta_proposals_impact_pending
  on public.meta_proposals (impact_score desc, created_at desc)
  where approval_status = 'pending';

create table if not exists public.meta_proposal_audit_log (
  id uuid primary key default gen_random_uuid(),
  proposal_id uuid references public.meta_proposals(id) on delete set null,
  action text not null,
  actor_id uuid references auth.users(id) on delete set null,
  actor_role text not null default coalesce(auth.role(), 'unknown'),
  previous_status public.meta_proposal_approval_status,
  new_status public.meta_proposal_approval_status,
  changes jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  constraint meta_proposal_audit_log_action_check check (
    action in ('created', 'updated', 'approved', 'rejected', 'reset_pending')
  )
);

create index if not exists idx_meta_proposal_audit_log_proposal_created
  on public.meta_proposal_audit_log (proposal_id, created_at desc);

create index if not exists idx_meta_proposal_audit_log_created
  on public.meta_proposal_audit_log (created_at desc);

create or replace function public.set_meta_proposal_review_fields()
returns trigger
language plpgsql
security invoker
set search_path = public, auth
as $$
begin
  new.updated_at = now();

  if tg_op = 'UPDATE' and new.approval_status is distinct from old.approval_status then
    if new.approval_status = 'pending' then
      new.reviewed_at = null;
      new.reviewed_by = null;
    else
      new.reviewed_at = coalesce(new.reviewed_at, now());
      new.reviewed_by = coalesce(new.reviewed_by, auth.uid());
    end if;
  end if;

  return new;
end;
$$;

drop trigger if exists set_meta_proposal_review_fields on public.meta_proposals;
create trigger set_meta_proposal_review_fields
before update on public.meta_proposals
for each row
execute function public.set_meta_proposal_review_fields();

create or replace function private.log_meta_proposal_audit()
returns trigger
language plpgsql
security definer
set search_path = public, auth
as $$
declare
  audit_action text;
begin
  if tg_op = 'INSERT' then
    insert into public.meta_proposal_audit_log (
      proposal_id,
      action,
      actor_id,
      actor_role,
      new_status,
      changes
    )
    values (
      new.id,
      'created',
      auth.uid(),
      coalesce(auth.role(), 'unknown'),
      new.approval_status,
      jsonb_build_object('row', to_jsonb(new))
    );
    return new;
  end if;

  if new.approval_status is distinct from old.approval_status then
    audit_action = case new.approval_status
      when 'approved' then 'approved'
      when 'rejected' then 'rejected'
      else 'reset_pending'
    end;
  else
    audit_action = 'updated';
  end if;

  insert into public.meta_proposal_audit_log (
    proposal_id,
    action,
    actor_id,
    actor_role,
    previous_status,
    new_status,
    changes
  )
  values (
    new.id,
    audit_action,
    auth.uid(),
    coalesce(auth.role(), 'unknown'),
    old.approval_status,
    new.approval_status,
    jsonb_strip_nulls(jsonb_build_object(
      'approval_status', case
        when new.approval_status is distinct from old.approval_status
          then jsonb_build_object('old', old.approval_status, 'new', new.approval_status)
      end,
      'reviewed_at', case
        when new.reviewed_at is distinct from old.reviewed_at
          then jsonb_build_object('old', old.reviewed_at, 'new', new.reviewed_at)
      end,
      'reviewed_by', case
        when new.reviewed_by is distinct from old.reviewed_by
          then jsonb_build_object('old', old.reviewed_by, 'new', new.reviewed_by)
      end,
      'proposal', case
        when new.proposed_meta_title is distinct from old.proposed_meta_title
          or new.proposed_meta_description is distinct from old.proposed_meta_description
          or new.rationale is distinct from old.rationale
          then jsonb_build_object(
            'proposed_meta_title', jsonb_build_object('old', old.proposed_meta_title, 'new', new.proposed_meta_title),
            'proposed_meta_description', jsonb_build_object('old', old.proposed_meta_description, 'new', new.proposed_meta_description),
            'rationale', jsonb_build_object('old', old.rationale, 'new', new.rationale)
          )
      end
    ))
  );

  return new;
end;
$$;

drop trigger if exists log_meta_proposal_audit_insert on public.meta_proposals;
create trigger log_meta_proposal_audit_insert
after insert on public.meta_proposals
for each row
execute function private.log_meta_proposal_audit();

drop trigger if exists log_meta_proposal_audit_update on public.meta_proposals;
create trigger log_meta_proposal_audit_update
after update on public.meta_proposals
for each row
execute function private.log_meta_proposal_audit();

alter table public.meta_proposals enable row level security;
alter table public.meta_proposal_audit_log enable row level security;

grant usage on schema public to authenticated, service_role;
grant usage on schema auth to authenticated, service_role;
grant select on table public.meta_proposals to authenticated;
grant update (approval_status) on table public.meta_proposals to authenticated;
grant select on table public.meta_proposal_audit_log to authenticated;
grant select, insert, update on table public.meta_proposals to service_role;
grant select, insert on table public.meta_proposal_audit_log to service_role;

drop policy if exists "authenticated can read meta proposals" on public.meta_proposals;
create policy "authenticated can read meta proposals"
on public.meta_proposals
for select
to authenticated
using ((select auth.uid()) is not null);

drop policy if exists "authenticated can review meta proposals" on public.meta_proposals;
create policy "authenticated can review meta proposals"
on public.meta_proposals
for update
to authenticated
using ((select auth.uid()) is not null)
with check ((select auth.uid()) is not null);

drop policy if exists "authenticated can read meta proposal audit" on public.meta_proposal_audit_log;
create policy "authenticated can read meta proposal audit"
on public.meta_proposal_audit_log
for select
to authenticated
using ((select auth.uid()) is not null);
