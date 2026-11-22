import { useParams } from "react-router-dom";
import { EmbeddedLinkPage } from "@multica/views/links";
export function LinkPage() {
  const {id} = useParams<{id: string}>();
  return id ? <EmbeddedLinkPage id={id} /> : null;
}
