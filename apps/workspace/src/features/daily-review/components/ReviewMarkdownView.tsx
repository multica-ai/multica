import ReactMarkdown from "react-markdown";
import type { DailyReview } from "@/shared/types";

interface Props {
  review: DailyReview;
}

/**
 * Renders a daily review's markdown draft content with basic prose styling.
 * Headings, lists, and paragraphs are styled using Tailwind utility classes.
 */
export function ReviewMarkdownView({ review }: Props) {
  return (
    <div className="prose prose-sm dark:prose-invert max-w-none text-sm leading-relaxed">
      <ReactMarkdown>{review.draft_content}</ReactMarkdown>
    </div>
  );
}
