import { CheckCircle, RefreshCw, CalendarDays } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "sonner";
import {
  useTomorrowPlanQuery,
  useGeneratePlanMutation,
  useConfirmPlanMutation,
} from "../hooks/use-daily-plan";
import { PlanMarkdownView } from "./PlanMarkdownView";

/**
 * Panel displayed on MyTimePage showing tomorrow's day plan draft.
 * Users can generate (or regenerate) the plan and confirm it when done.
 */
export function DailyPlanPanel() {
  const { data: plan, isLoading } = useTomorrowPlanQuery();
  const generate = useGeneratePlanMutation();
  const confirm = useConfirmPlanMutation();

  const handleGenerate = () => {
    generate.mutate(undefined, {
      onSuccess: () => toast.success("明日计划已生成"),
      onError: () => toast.error("计划生成失败，请重试"),
    });
  };

  const handleConfirm = () => {
    if (!plan) return;
    confirm.mutate(plan.id, {
      onSuccess: () => toast.success("计划已确认"),
      onError: () => toast.error("确认失败，请重试"),
    });
  };

  return (
    <div className="rounded-lg border bg-card p-4 space-y-3">
      {/* Header row */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-sm font-medium">
          <CalendarDays className="h-4 w-4 text-muted-foreground" />
          <span>明日计划</span>
          {plan?.status === "confirmed" && (
            <CheckCircle className="h-4 w-4 text-green-500" />
          )}
        </div>
        <div className="flex items-center gap-2">
          {plan?.status !== "confirmed" && plan && (
            <Button
              size="sm"
              variant="outline"
              onClick={handleConfirm}
              disabled={confirm.isPending}
            >
              <CheckCircle className="mr-1 h-3 w-3" />
              确认
            </Button>
          )}
          <Button
            size="sm"
            variant="outline"
            onClick={handleGenerate}
            disabled={generate.isPending}
          >
            <RefreshCw className={`mr-1 h-3 w-3 ${generate.isPending ? "animate-spin" : ""}`} />
            {plan ? "重新生成" : "生成计划"}
          </Button>
        </div>
      </div>

      {/* Content area */}
      {isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-4 w-5/6" />
        </div>
      ) : plan ? (
        <PlanMarkdownView plan={plan} />
      ) : (
        <p className="text-sm text-muted-foreground">
          明日计划尚未生成。点击「生成计划」查看 AI 为你规划的明天。
        </p>
      )}
    </div>
  );
}
