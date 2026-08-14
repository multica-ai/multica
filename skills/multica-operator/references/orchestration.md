# Business Goal Orchestration

Use this workflow for an open-ended business outcome, workflow or automation
design, resource selection, task decomposition, or multiple dependent
mutations. Route by intent, not resource count. Keep a concrete action on a
known target in the direct-operation flow. This is business execution
orchestration, not a software development project.

## Clarify and decompose

Resolve the requested profile and workspace first, and preserve any explicit
server target. Do not claim an identity or effective server that the community
CLI cannot verify safely. Clarify only facts that materially change the design:
outcome, deliverable, frequency, data sources, constraints, deadline, and
acceptance criteria.

Classify the work:

- **one-time:** an Issue, optionally in a Project or assigned to an Agent;
- **recurring:** an Autopilot whose description is the task prompt;
- **coordinated:** a parent Issue with staged or parked child Issues, optionally
  coordinated by a Squad leader Agent.

The request authorizes relevant read-only discovery for the plan, not an
unrelated full-workspace inventory or any write. Query only relevant Projects,
Issues, Agents, Squads, Skills, and Autopilots with structured CLI output.
Start with list or search, then get only plausible candidates. Compare actual
purpose, instructions, bindings, membership, status, and active use; do not
select by name alone.

## Select resources conservatively

Classify every resource proposed for modification:

- **dedicated:** read evidence shows it is scoped to this goal with no known
  active external dependency;
- **shared:** evidence shows another Project, Squad, Autopilot, active Issue, or
  workflow depends on it;
- **unknown:** available CLI reads cannot establish either state.

Treat unknown as shared for mutation decisions. Modify a dedicated resource by
default only when the evidence is sufficient. Keep shared and unknown resources
unchanged and prefer an isolated resource. If alternatives remain reasonable,
show their impact and let the user choose.

Prefer existing resources that match the goal. Do not duplicate unfinished
Issues. Use a Squad only when coordination or role separation adds value.
Autopilots assign Agents, not Squads. For recurring coordinated work, assign the
Autopilot Issue to the Squad leader Agent; the leader coordinates member Agents
through child Issues.

## Protect Skills and Agents

Inspect and reuse a Skill only as it is already installed and configured. The
Operator must not create, update, import, or delete a Skill. Binding or
unbinding an existing Skill changes the Agent configuration, not the Skill
definition. Include the exact binding delta in the complete plan and apply the
separate Agent confirmation before changing it.

Prefer an existing capable Agent. Do not create one for simple one-time work
that an existing Agent can execute from a clear Issue description. Every Agent
create or update requires both inclusion in the complete plan and a separate
second confirmation immediately before the CLI mutation. Show the exact delta,
but never secret values. Without that confirmation, skip the Agent mutation and
all dependent steps.

Assigning an unchanged Agent to an Issue or adding it to a Squad uses the Agent
without changing its configuration; plan-level confirmation is sufficient.

## Handle a missing Skill

Offer the user two choices:

1. use a Temporary embedded instruction in an Issue or Autopilot description;
2. stop while the user creates or imports a maintained Skill in Multica Web.

Use temporary text only when an existing Agent already has the necessary
tools, permissions, credentials, and data access. Instructions cannot create a
missing capability.

```markdown
## Temporary embedded instruction

Goal:
Data sources and inputs:
Execution steps:
Output format:
Failure and exception handling:
Acceptance criteria:
Required tools and permissions:

> Temporary solution: move stable, recurring rules into a maintained Skill for
> long-term operation.
```

For recurring work, put the complete instruction in the Autopilot description;
that description is the task prompt used for created Issues. Do not depend on a
separate example Issue being copied later.

## Present and confirm the plan

The in-chat orchestration design is the execution plan. Present it, get one
user confirmation, and then move directly to execution. After the user confirms
it, execute the plan directly in dependency order. Do not insert a repository
documentation, specification review, or software implementation-planning stage
between confirmation and execution.

Create a repository design document, specification, or implementation plan only
when the user explicitly requests that artifact. Such a document records the
approved orchestration; it is not a second approval gate unless the user asks
for another review.

Before any write, present one complete plan containing:

- goal, deliverables, frequency, and acceptance criteria;
- task breakdown and dependencies;
- relevant existing resources, selection reasons, and sharing evidence;
- every create, update, assignment, membership, and trigger mutation;
- temporary instructions and capability limitations;
- the executable Agent, Squad, Issue, Project, and Autopilot relationships;
- CLI-unsupported steps that must be completed in Multica Web;
- Agent changes needing separate confirmation;
- risks and long-term recommendations.

One confirmation authorizes only those mutations in the stated workspace. A
material deviation in fields, target, resource choice, dependency, or impact
stops execution before the changed step. Show the difference and request a
revised confirmation; never adapt silently.

## Execute, activate, and resume

Execute approved steps in dependency order through the installed `multica` CLI
only. Record each step's intent, status, verification, returned resource ID,
resource type, and display name. Exclude secrets. Use returned IDs for all later
commands.

Keep setup passive until dependencies are ready:

1. create or verify passive dependencies;
2. create parked child Issues as `backlog`;
3. create an Autopilot only after its Agent, Project, and prompt are ready;
4. add or enable its trigger last;
5. move approved Issues to `todo` only when dependencies are ready.

On failure, stop dependent steps. Report completed resources and IDs, the
observed error, steps not run, and the exact resume point. To resume, get every
completed resource by its recorded ID and verify it before continuing. Never
recreate a resource by name. If a resource is missing or materially changed,
stop and request confirmation.

If the CLI lacks a required operation, label it as a Multica Web step. Wait for
the user to confirm that Web step before continuing its dependents. Never read
a profile token or call the API directly as a fallback.
