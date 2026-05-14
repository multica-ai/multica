"use client";

import { useState } from "react";
import { Bot, Users, User } from "lucide-react";

import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from "@multica/ui/components/ui/table";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";

import {
  ALL_PERMISSIONS,
  PERMISSION_LABEL,
  type CredentialPolicy,
  type Permission,
  type SubjectKind,
} from "../types";

function subjectIcon(kind: SubjectKind) {
  switch (kind) {
    case "member":
      return <User className="size-3.5 text-muted-foreground" />;
    case "agent":
      return <Bot className="size-3.5 text-muted-foreground" />;
    case "group":
      return <Users className="size-3.5 text-muted-foreground" />;
  }
}

export function PolicyEditor({
  credentialId,
  initialPolicies,
}: {
  credentialId: string;
  initialPolicies: CredentialPolicy[];
}) {
  const [policies, setPolicies] = useState(initialPolicies);
  const [dirty, setDirty] = useState(false);

  function togglePermission(
    subjectId: string,
    subjectKind: SubjectKind,
    permission: Permission,
  ) {
    setDirty(true);
    setPolicies((prev) =>
      prev.map((p) => {
        if (p.subject_id !== subjectId || p.subject_kind !== subjectKind) {
          return p;
        }
        const has = p.permissions.includes(permission);
        return {
          ...p,
          permissions: has
            ? p.permissions.filter((x) => x !== permission)
            : [...p.permissions, permission],
        };
      }),
    );
  }

  // STUB: persistence depends on JEH-1179 (Persona grants admin API at
  // `/api/workspaces/{id}/grants`). Per JEH-1197 (policy-enforcement), the
  // credential policy checker reads Persona grants — so "set a credential
  // policy" in this UI is, at the data layer, "create/update a Persona
  // grant against the credential". Until JEH-1179 lands, the save button
  // stays disabled with the tooltip below.
  const saveBlocked = true;
  async function handleSave() {
    setDirty(false);
  }

  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground">
        Tick a permission to grant it. Saves go through{" "}
        <code className="font-mono">credential_policy_set</code> when the
        backend ships ({credentialId}).
      </p>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Subject</TableHead>
            {ALL_PERMISSIONS.map((p) => (
              <TableHead key={p} className="text-center text-xs">
                {PERMISSION_LABEL[p]}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {policies.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={ALL_PERMISSIONS.length + 1}
                className="py-6 text-center text-sm text-muted-foreground"
              >
                No subjects bound yet.
              </TableCell>
            </TableRow>
          ) : (
            policies.map((policy) => (
              <TableRow
                key={`${policy.subject_kind}:${policy.subject_id}`}
                data-testid={`policy-row-${policy.subject_id}`}
              >
                <TableCell>
                  <span className="flex items-center gap-2">
                    {subjectIcon(policy.subject_kind)}
                    <span>{policy.subject_label}</span>
                    <span className="text-xs text-muted-foreground">
                      ({policy.subject_kind})
                    </span>
                  </span>
                </TableCell>
                {ALL_PERMISSIONS.map((perm) => {
                  const checked = policy.permissions.includes(perm);
                  return (
                    <TableCell key={perm} className="text-center">
                      <Checkbox
                        checked={checked}
                        onCheckedChange={() =>
                          togglePermission(
                            policy.subject_id,
                            policy.subject_kind,
                            perm,
                          )
                        }
                        aria-label={`${PERMISSION_LABEL[perm]} for ${policy.subject_label}`}
                      />
                    </TableCell>
                  );
                })}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>

      <div className="flex justify-end">
        <Tooltip>
          <TooltipTrigger render={<span tabIndex={0} />}>
            <Button
              disabled={!dirty || saveBlocked}
              onClick={handleSave}
              size="sm"
            >
              Save policy
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            Saving is disabled until the Persona grants admin API
            (JEH-1179) is wired. Use `multica autopilot` or the persona-mcp
            tools for now.
          </TooltipContent>
        </Tooltip>
      </div>
    </div>
  );
}
