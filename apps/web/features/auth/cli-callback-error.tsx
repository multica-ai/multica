"use client";

import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
} from "@multica/ui/components/ui/card";
import { useT } from "@multica/views/i18n";

export function CliCallbackError() {
  const { t } = useT("auth");
  return (
    <div className="flex min-h-screen items-center justify-center">
      <Card className="w-full max-w-sm" role="alert">
        <CardHeader className="text-center">
          <CardTitle className="text-display-sm">
            {t(($) => $.web.cli_callback.failed_title)}
          </CardTitle>
          <CardDescription>
            {t(($) => $.web.cli_callback.invalid_description)}
          </CardDescription>
        </CardHeader>
      </Card>
    </div>
  );
}
