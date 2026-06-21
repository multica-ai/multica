import { api } from "@multica/core/api";
import type { Chapter, ChapterHandoffInput, ChapterStatus } from "./types";

const base = (issueId: string) => `/api/cerebro/issues/${issueId}/chapters`;

export function listChapters(issueId: string): Promise<Chapter[]> {
  return api.cerebroRequest<Chapter[]>(base(issueId));
}

export function createChapter(issueId: string, input: {
  name: string;
  status?: ChapterStatus;
  handoff?: ChapterHandoffInput;
}): Promise<Chapter> {
  return api.cerebroRequest<Chapter>(base(issueId), {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function updateChapter(issueId: string, chapterId: string, input: {
  name?: string;
  status?: ChapterStatus;
  position?: number;
  handoff?: ChapterHandoffInput;
}): Promise<Chapter> {
  return api.cerebroRequest<Chapter>(`${base(issueId)}/${chapterId}`, {
    method: "PATCH",
    body: JSON.stringify(input),
  });
}

export function startFresh(issueId: string, input: ChapterHandoffInput): Promise<Chapter> {
  return api.cerebroRequest<Chapter>(`${base(issueId)}/start-fresh`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}
