import {
  useMutation,
  type MutateOptions,
  type MutationFunctionContext,
  type UseMutationOptions,
  type UseMutationResult,
} from "@tanstack/react-query";
import { assertCurrentSession, currentSessionEpoch, sessionEpochChanged } from "./session-epoch";

interface SessionVariables<T> {
  value: T;
  epoch: number;
}

interface SessionMutationContext extends MutationFunctionContext {
  /** Check again after awaiting inside a lifecycle callback. */
  assertCurrentSession: () => void;
}

type SessionOptions<D, E, V, R> = Omit<
  UseMutationOptions<D, E, V, R>,
  "mutationFn" | "onMutate" | "onSuccess" | "onError" | "onSettled"
> & {
  mutationFn: (variables: V, context: SessionMutationContext) => Promise<D>;
  onMutate?: (variables: V, context: SessionMutationContext) => R | Promise<R>;
  onSuccess?: (data: D, variables: V, result: R, context: SessionMutationContext) => unknown;
  onError?: (error: E, variables: V, result: R | undefined, context: SessionMutationContext) => unknown;
  onSettled?: (data: D | undefined, error: E | null, variables: V, result: R | undefined, context: SessionMutationContext) => unknown;
};

function sessionContext(context: MutationFunctionContext, epoch: number): SessionMutationContext {
  return { ...context, assertCurrentSession: () => assertCurrentSession(epoch) };
}

/**
 * Keep each invocation's epoch with its variables, including offline/queued
 * mutations and observer option updates. Clearing MutationCache does not stop
 * pending lifecycle callbacks, so both dispatch and callbacks need this boundary.
 */
export function useSessionMutation<D = unknown, E = Error, V = void, R = unknown>(
  options: SessionOptions<D, E, V, R>,
): UseMutationResult<D, E, V, R> {
  const mutation = useMutation<D, E, SessionVariables<V>, R>({
    ...options,
    mutationFn: async ({ value, epoch }, context) => {
      assertCurrentSession(epoch);
      const data = await options.mutationFn(value, sessionContext(context, epoch));
      assertCurrentSession(epoch);
      return data;
    },
    onMutate: options.onMutate ? async ({ value, epoch }, context) => {
      assertCurrentSession(epoch);
      const result = await options.onMutate!(value, sessionContext(context, epoch));
      assertCurrentSession(epoch);
      return result;
    } : undefined,
    onSuccess: (data, { value, epoch }, result, context) => {
      if (!sessionEpochChanged(epoch)) return options.onSuccess?.(data, value, result, sessionContext(context, epoch));
    },
    onError: (error, { value, epoch }, result, context) => {
      if (!sessionEpochChanged(epoch)) return options.onError?.(error, value, result, sessionContext(context, epoch));
    },
    onSettled: (data, error, { value, epoch }, result, context) => {
      if (!sessionEpochChanged(epoch)) return options.onSettled?.(data, error, value, result, sessionContext(context, epoch));
    },
  });

  const callbacks = (options?: MutateOptions<D, E, V, R>): MutateOptions<D, E, SessionVariables<V>, R> => ({
    onSuccess: (data, { value, epoch }, result, context) => {
      if (!sessionEpochChanged(epoch)) options?.onSuccess?.(data, value, result, context);
    },
    onError: (error, { value, epoch }, result, context) => {
      if (!sessionEpochChanged(epoch)) options?.onError?.(error, value, result, context);
    },
    onSettled: (data, error, { value, epoch }, result, context) => {
      if (!sessionEpochChanged(epoch)) options?.onSettled?.(data, error, value, result, context);
    },
  });

  return {
    ...mutation,
    variables: mutation.variables?.value,
    mutate: (value, options) =>
      mutation.mutate({ value, epoch: currentSessionEpoch() }, callbacks(options)),
    mutateAsync: (value, options) =>
      mutation.mutateAsync({ value, epoch: currentSessionEpoch() }, callbacks(options)),
  } as UseMutationResult<D, E, V, R>;
}
