import { useParams } from "react-router-dom";
import { AppDetailPage as SharedAppDetailPage } from "@multica/views/apps";

export function AppDetailPage() {
  const { slug } = useParams<{ slug: string }>();
  if (!slug) return null;
  return <SharedAppDetailPage toolkitSlug={slug} />;
}
