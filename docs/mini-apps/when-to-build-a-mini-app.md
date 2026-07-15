# When to build a mini app

Build a mini app when all of these are true:

- Its business data lives in the Firtal Data Registry or approved Multica Connections.
- Its users are internal.
- It does not need its own public domain, independent database, or public access.
- It can operate inside Multica's release, availability, identity, and permission model.

Build a separately deployed app when any of these are true:

- Customers or other external users must reach it.
- It owns a data model outside the Registry.
- It needs its own domain, service-level commitment, or independent release cycle.
- It must keep operating when Multica is unavailable.

Choose the smaller architecture that completely satisfies the business requirement. Do not use a mini app merely to avoid defining ownership, data scopes, or operational responsibility.

