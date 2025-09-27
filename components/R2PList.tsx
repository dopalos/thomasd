// components/R2PList.tsx (React)
import { useEffect, useRef, useState } from "react";
import { fetchR2PPage, type R2PRecord } from "../api/r2p";

export default function R2PList() {
  const base = "http://127.0.0.1:8081";
  const [items, setItems] = useState<R2PRecord[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [loading, setLoading] = useState(false);
  const [done, setDone] = useState(false);
  const sentinelRef = useRef<HTMLDivElement>(null);

  async function loadMore() {
    if (loading || done) return;
    setLoading(true);
    const page = await fetchR2PPage(base, {
      owner: "tho1alice", role: "payee", status: "paid", limit: 20, cursor,
    });
    setItems(prev => [...prev, ...page.records]);
    if (!page.next_cursor) setDone(true);
    setCursor(page.next_cursor);
    setLoading(false);
  }

  useEffect(() => {
    loadMore(); // first page
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return;
    const io = new IntersectionObserver((e) => {
      if (e[0].isIntersecting) loadMore();
    });
    io.observe(el);
    return () => io.disconnect();
  }, [sentinelRef.current, cursor, loading, done]);

  return (
    <div className="max-w-xl mx-auto p-4 space-y-3">
      {items.map(r => (
        <div key={r.id} className="rounded-2xl shadow p-3">
          <div className="text-sm opacity-70">{r.id}</div>
          <div className="text-base font-medium">{r.from} ??{r.to}</div>
          <div className="text-sm">status: {r.status}</div>
        </div>
      ))}
      {!done && <div ref={sentinelRef} className="py-6 text-center">{loading ? "Loading?? : " "}</div>}
      {done && <div className="py-6 text-center text-sm opacity-60">end of list</div>}
    </div>
  );
}

