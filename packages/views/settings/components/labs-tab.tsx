"use client";

import { FlaskConical } from "lucide-react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useT } from "@multica/i18n/react";

export function LabsTab() {
  const t = useT("settings");

  return (
    <div className="space-y-4">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t("labs")}</h2>

        <Card>
          <CardContent>
            <div className="flex items-start gap-3">
              <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">
                <FlaskConical className="h-4 w-4" />
              </div>
              <div className="space-y-1">
                <p className="text-sm font-medium">{t("tokens_no_experimental")}</p>
                <p className="text-sm text-muted-foreground">
                  {t("tokens_no_experimental_desc")}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
