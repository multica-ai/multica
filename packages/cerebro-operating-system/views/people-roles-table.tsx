"use client";

import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";

// One row of the People tab: a person or an agent, and every role they hold.
export interface RoleHolder {
  key: string;
  name: string;
  type: "member" | "agent";
  initials: string;
  avatarUrl?: string | null;
  roles: string[];
}

export function PeopleRolesTable({ holders, vacantRoles }: { holders: RoleHolder[]; vacantRoles: string[] }) {
  if (holders.length === 0) return <p className="rounded-xl border border-dashed p-8 text-center text-sm text-muted-foreground">No people or agents in this workspace yet.</p>;

  return (
    <div className="grid gap-6">
      <div className="overflow-x-auto rounded-xl border">
        <table className="w-full text-sm">
          <caption className="sr-only">People and their roles</caption>
          <thead className="bg-muted/50 text-left text-xs uppercase tracking-wide text-muted-foreground">
            <tr>
              <th scope="col" className="px-4 py-2 font-medium">Name</th>
              <th scope="col" className="px-4 py-2 font-medium">Type</th>
              <th scope="col" className="px-4 py-2 font-medium">Roles</th>
            </tr>
          </thead>
          <tbody>
            {holders.map((holder) => (
              <tr key={holder.key} className="border-t align-top">
                <td className="px-4 py-2">
                  <span className="flex items-center gap-2">
                    <ActorAvatar name={holder.name} initials={holder.initials} avatarUrl={holder.avatarUrl} isAgent={holder.type === "agent"} size={24} />
                    <span className="font-medium">{holder.name}</span>
                  </span>
                </td>
                <td className="px-4 py-2 text-muted-foreground">{holder.type === "agent" ? "Agent" : "Person"}</td>
                <td className="px-4 py-2">
                  {holder.roles.length > 0
                    ? <span className="flex flex-wrap gap-1">{holder.roles.map((role) => <span key={role} className="rounded-md border bg-muted/40 px-2 py-0.5 text-xs">{role}</span>)}</span>
                    : <span className="text-xs text-muted-foreground/70">No role yet</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {vacantRoles.length > 0 && (
        <section aria-label="Vacant roles" className="grid gap-2 rounded-xl border border-dashed p-4">
          <h3 className="text-sm font-semibold">Vacant roles</h3>
          <p className="flex flex-wrap gap-1">{vacantRoles.map((role) => <span key={role} className="rounded-md border px-2 py-0.5 text-xs text-muted-foreground">{role}</span>)}</p>
        </section>
      )}
    </div>
  );
}
