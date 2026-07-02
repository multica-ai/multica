"use client";

import { useQuery } from "@tanstack/react-query";
import { planApi } from "@multica/core/api/workflows";
import type { Plan } from "@multica/core/types/workflow";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Card } from "@multica/ui/components/ui/card";
import { useNavigation } from "@multica/views/navigation";

export function PlanListPage() {
  const wsId = useWorkspaceId();
  const navigation = useNavigation();

  const { data: plans, isLoading } = useQuery<Plan[]>({
    queryKey: ["plans", wsId],
    queryFn: () => planApi.list(wsId),
  });

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold">Plans</h1>
        <Button onClick={() => {
          // Navigate to new plan creation — for now just use a placeholder
          // The new plan page will be added in a future phase
        }}>
          New Plan
        </Button>
      </div>
      {isLoading ? (
        <div>Loading...</div>
      ) : plans?.length === 0 ? (
        <div className="text-muted-foreground">No plans yet.</div>
      ) : (
        <div className="grid gap-4">
          {plans?.map((plan) => (
            <Card
              key={plan.id}
              className="p-4 cursor-pointer hover:bg-muted/50 transition-colors"
              onClick={() => navigation.push(`/${wsId}/plans/${plan.id}`)}
            >
              <h2 className="font-medium">{plan.title}</h2>
              <p className="text-sm text-muted-foreground mt-1">
                {plan.status} — {new Date(plan.created_at).toLocaleDateString()}
              </p>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
