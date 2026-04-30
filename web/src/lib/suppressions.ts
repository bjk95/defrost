import { useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  addSuppression,
  getSuppressions,
  removeSuppression,
} from "@/api";

export interface SuppressionEntry {
  test_id: string;
}

const KEY = ["suppressions"] as const;

export interface UseSuppressionsResult {
  entries: SuppressionEntry[];
  ids: Set<string>;
  has: (testId: string) => boolean;
  count: number;
  isLoading: boolean;
  isError: boolean;
  error: Error | null;
  isMutating: boolean;
  add: (testId: string) => void;
  remove: (testId: string) => void;
}

export function useSuppressions(): UseSuppressionsResult {
  const qc = useQueryClient();
  const { data, isLoading, isError, error } = useQuery({
    queryKey: KEY,
    queryFn: getSuppressions,
    staleTime: 0,
  });

  const ids = useMemo(() => new Set(data?.test_ids ?? []), [data]);
  const entries = useMemo<SuppressionEntry[]>(
    () => (data?.test_ids ?? []).map((test_id) => ({ test_id })),
    [data],
  );

  // Each mutation response is the authoritative current list, so we
  // write it straight into the cache. Only invalidate on error — that
  // forces a fresh read to reconcile, instead of incurring an extra
  // git-backed GET on every successful add/remove.
  const addMut = useMutation({
    mutationFn: addSuppression,
    onSuccess: (resp) => qc.setQueryData(KEY, resp),
    onError: () => qc.invalidateQueries({ queryKey: KEY }),
  });
  const removeMut = useMutation({
    mutationFn: removeSuppression,
    onSuccess: (resp) => qc.setQueryData(KEY, resp),
    onError: () => qc.invalidateQueries({ queryKey: KEY }),
  });

  return {
    entries,
    ids,
    has: (testId: string) => ids.has(testId),
    count: ids.size,
    isLoading,
    isError,
    error: (error as Error | null) ?? null,
    isMutating: addMut.isPending || removeMut.isPending,
    add: (testId: string) => addMut.mutate(testId),
    remove: (testId: string) => removeMut.mutate(testId),
  };
}
