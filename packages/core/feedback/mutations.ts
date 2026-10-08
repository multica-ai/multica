import { useMutation } from "@tanstack/react-query";
import { api } from "../api";
import type { FeedbackContext, FeedbackKind } from "./types";

export interface CreateFeedbackInput {
  message: string;
  url?: string;
  workspace_id?: string;
  kind?: FeedbackKind;
  context?: FeedbackContext;
}

export function useCreateFeedback() {
  return useMutation({
    mutationFn: (input: CreateFeedbackInput) => api.createFeedback(input),
  });
}
