"use client";

import { FlaskConical } from "lucide-react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useSettingsT } from "../i18n";

export function LabsTab() {
  const t = useSettingsT();
  return (
    <div className="space-y-4">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t.labs.sectionTitle}</h2>

        <Card>
          <CardContent>
            <div className="flex items-start gap-3">
              <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">
                <FlaskConical className="h-4 w-4" />
              </div>
              <div className="space-y-1">
                <p className="text-sm font-medium">{t.labs.emptyTitle}</p>
                <p className="text-sm text-muted-foreground">
                  {t.labs.emptyDescription}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
