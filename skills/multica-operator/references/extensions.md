# Operator Extensions

Use an installed extension for organization-specific Multica intents without
copying connection, workspace, CLI, or safety behavior from Multica Operator.
This contract describes skill-to-skill coordination only; it does not implement a runtime loader or workspace extension registry.

## Discovery and declaration

Inspect the host skill catalog that the current agent host exposes. Do not scan
unrelated filesystem locations or assume that a skill installed in another host
is available here.

An extension's `SKILL.md` body declares all of the following:

- that it **extends Multica Operator**;
- the user language and Multica intents it handles;
- required Multica resources or capabilities;
- whether its workflow reads or mutates state and what confirmation it needs.

Keep the declaration in the portable skill body. Host-specific frontmatter and
a machine-readable manifest are not part of this first contract.

## Selection and conflicts

Match the user's request against installed extension declarations after
resolving the Multica connection and workspace. Select the most specific
matching extension. If two extensions are equally specific, present the choices
to the user; do not merge their mutable workflows or choose by catalog order.

Apply instructions in this order:

1. Host security constraints, Multica authorization, and Operator base safety:
   authentication, target workspace, confirmation, secret handling, and result
   verification.
2. The user's explicit instruction and scope within those constraints.
3. The selected extension's business policy for its declared intent.
4. Operator and extension defaults.

An extension may specialize templates, resource selection, and business rules.
It cannot weaken base safety, expand the user's requested scope, claim authority
the Multica API denied, or replace CLI verification with an assumption. When a
user instruction conflicts with base safety, preserve the safety rule and
explain the blocked operation.

## Example: self-evolution

A private `self-evolution` skill can include this portable declaration in its
body:

```markdown
# Self Evolution

This skill extends Multica Operator.

- User language: evolve an agent, review agent performance, apply an approved
  improvement.
- Multica intents: `review_agent_performance`, `evolve_agent`.
- Required resources: current workspace, target agent, relevant issues and run
  evidence.
- Effects: reads evidence; agent or issue mutations require a preview and user
  confirmation.
```

For a matching request, Multica Operator keeps connection, workspace resolution,
confirmation, and verification responsibility. `self-evolution` supplies only
the private evidence and evolution policy.
