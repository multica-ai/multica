import { useMutation } from "@tanstack/react-query";
import { api } from "../api";

export interface CreateFeedbackInput {
  message: string;
  url?: string;
  workspace_id?: string;
}

export function useCreateFeedback() {
  return useMutation({
    mutationFn: (input: CreateFeedbackInput) => api.createFeedback(input),
  });
}
