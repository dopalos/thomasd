// app/hooks/useR2PInfinite.ts
"use client";
import { useInfiniteQuery } from "@tanstack/react-query";

export function useR2PInfinite(
  base: string,
  params: { owner: string; role: string; status?: string; limit?: number }
) {
  return useInfiniteQuery({
    queryKey: ["r2p", base, params],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const u = new URL(`${base.replace(/\/$/,"")}/api/r2p`);
      Object.entries({ ...params, cursor: pageParam })
        .forEach(([k, v]) => v != null && u.searchParams.set(k, String(v)));
      const res = await fetch(u.toString(), { cache: "no-store" });
      if (!res.ok) throw new Error(`r2p ${res.status}`);
      const json: any = await res.json().catch(() => ({}));

      // ✅ records 정규화
      const records =
        Array.isArray(json?.records) ? json.records :
        Array.isArray(json?.data)    ? json.data :
        Array.isArray(json)          ? json : [];
      const nextCursor = json?.nextCursor ?? json?.next ?? null;

      return { records, nextCursor };
    },
    getNextPageParam: (last) => last?.nextCursor ?? undefined,
    select: (data) => ({
      ...data,
      pages: data.pages.map((p: any) => ({
        records: Array.isArray(p?.records) ? p.records : [],
        nextCursor: p?.nextCursor ?? null,
      })),
    }),
  });
}
