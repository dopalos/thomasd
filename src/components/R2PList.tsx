import { useEffect, useRef } from "react";
import { useR2PInfinite } from "../hooks/useR2PInfinite";

export default function R2PList() {
  const base = import.meta.env.VITE_API_BASE ?? "http://127.0.0.1:8081";
  const { data, fetchNextPage, hasNextPage, isFetchingNextPage } = useR2PInfinite(base, {
    owner: "tho1alice", role: "payee", status: "paid", limit: 20,
  });

  const items = data?.pages.flatMap(p => p.records) ?? [];
  const sentinel = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = sentinel.current; if (!el) return;
    const io = new IntersectionObserver((entries) => {
      if (entries[0].isIntersecting && hasNextPage && !isFetchingNextPage) fetchNextPage();
    });
    io.observe(el);
    return () => io.disconnect();
  }, [sentinel, hasNextPage, isFetchingNextPage, fetchNextPage]);

  return (
    <div className="max-w-xl mx-auto p-4 space-y-3">
      {items.map(r => (
        <div key={r.id} className="rounded-2xl shadow p-3">
          <div className="text-sm opacity-60">{r.id}</div>
          <div className="font-medium">{r.from} ??{r.to}</div>
          <div className="text-sm">status: {r.status}</div>
        </div>
      ))}
      {!hasNextPage ? (
        <div className="py-6 text-center text-sm opacity-60">end of list</div>
      ) : (
        <div ref={sentinel} className="py-6 text-center">
          {isFetchingNextPage ? "Loading?? : " "}
        </div>
      )}
    </div>
  );
}

