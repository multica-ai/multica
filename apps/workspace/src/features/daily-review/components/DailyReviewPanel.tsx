import { CheckCircle, RefreshCw, BookOpen } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "sonner";
import {
  useTodayReviewQuery,
  useGenerateReviewMutation,
  useConfirmReviewMutation,
} from "../hooks/use-daily-review";
import { ReviewMarkdownView } from "./ReviewMarkdownView";

/**
 * Panel displayed on MyTimePage showing today's nightly review draft.
 * Users can generate (or regenerate) the draft and confirm it when done.
 */
export function DailyReviewPanel() {
  const { data: review, isLoading } = useTodayReviewQuery();
  const generate = useGenerateReviewMutation();
  const confirm = useConfirmReviewMutation();

  const handleGenerate = () => {
    generate.mutate(undefined, {
      onSuccess: () => toast.success("今日复盘已生成"),
      onError: () => toast.error("复盘生成失败，请重试"),
    });
  };

  const handleConfirm = () => {
    if (!review) return;
    confirm.mutate(review.id, {
      onSuccess: () => toast.success("复盘已确认"),
      onError: () => toast.error("确认失败，请重试"),
    });
  };

  return (
    <div className="rounded-lg border bg-card p-4 space-y-3">
      {/* Header row */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-sm font-medium">
          <BookOpen className="h-4 w-4 text-muted-foreground" />
          <span>今日复盘</span>
          {review?.status === "confirmed" && (
            <CheckCircle className="h-4 w-4 text-green-500" />
          )}
        </div>
        <div className="flex items-center gap-2">
          {review?.status !== "confirmed" && review && (
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
            {review ? "重新生成" : "生成复盘"}
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
      ) : review ? (
        <ReviewMarkdownView review={review} />
      ) : (
        <p className="text-sm text-muted-foreground">
          今日复盘尚未生成。点击「生成复盘」查看 AI 为你准备的回顾。
        </p>
      )}
    </div>
  );
}
