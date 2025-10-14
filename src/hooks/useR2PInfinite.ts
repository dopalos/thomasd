import { useInfiniteQuery } from "@tanstack/react-query";
import { fetchR2PPage, type R2PPage } from "../api/r2p";

export function useR2PInfinite(base: string, p: {
  owner: string; role?: "payee"|"payer"|"any"; status?: "open"|"paid"|"declined"|"canceled"; limit?: number;
}) {
  return useInfiniteQuery<R2PPage, Error>({
    queryKey: ["r2p", p],
    queryFn: ({ pageParam }) => fetchR2PPage(base, { ...p, cursor: pageParam as string | undefined }),
    initialPageParam: undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

