"use client";

import { useState } from "react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@multica/ui/components/ui/card";
import { useAuthStore } from "@multica/core/auth";
import { useT } from "../i18n";
import { DeviceCodeApproval } from "./device-code-approval";

// Device authorization approval page (`multica login --device` prints its
// URL). The code entry and confirmation step live in DeviceCodeApproval.
export function ActivatePage() {
  const { t } = useT("auth");
  const user = useAuthStore((s) => s.user);
  const [done, setDone] = useState(false);

  if (done) {
    return (
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-display-sm">
            {t(($) => $.activate.approved_title)}
          </CardTitle>
          <CardDescription>
            {t(($) => $.activate.approved_description)}
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  return (
    <Card className="w-full max-w-sm">
      <CardHeader className="text-center">
        <CardTitle className="text-display-sm">
          {t(($) => $.activate.title)}
        </CardTitle>
        <CardDescription>{t(($) => $.activate.description)}</CardDescription>
      </CardHeader>
      <CardContent>
        <DeviceCodeApproval autoFocus onApproved={() => setDone(true)} />
      </CardContent>
      <CardFooter className="justify-center">
        {user?.email && (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.activate.approving_as, { email: user.email })}
          </p>
        )}
      </CardFooter>
    </Card>
  );
}
