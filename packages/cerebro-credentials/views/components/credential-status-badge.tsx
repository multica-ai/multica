"use client";

import { Badge } from "@multica/ui/components/ui/badge";
import type { CredentialStatus } from "../types";

export function CredentialStatusBadge({ status }: { status: CredentialStatus }) {
  switch (status) {
    case "active":
      return <Badge variant="secondary">Active</Badge>;
    case "expired":
      return <Badge variant="destructive">Expired</Badge>;
    case "revoked":
      return <Badge variant="outline">Revoked</Badge>;
    default:
      return <Badge variant="outline">{status as string}</Badge>;
  }
}
