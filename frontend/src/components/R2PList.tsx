// C:\thomas-scaffold\app\components\R2PList.tsx
"use client";

import { useEffect, useRef, useState } from "react";
import { useR2PInfinite } from "../hooks/useR2PInfinite";
import { approveR2P, cancelR2P, commitRound, declineR2P } from "../lib/r2p";
import R2PForm from "./R2PForm";

// ---- 타입 ----
type Role = "payee" | "payer" | "any";
type Status = "open" | "paid" | "declined" | "canceled" | "";

// ---- 아주 가벼운 토스트 (의존성 X) ----
function MiniToast({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <div
      style={{
        position: "fixed",
        left: "50%",
        bottom: 20,
        transform: "translateX(-50%)",
        background: "rgba(0,0,0,0.8)",
        color: "#fff",
        padding: "10px 14px",
        borderRadius: 10,
        fontSize: 13,
        zIndex: 9999,
        maxWidth: 640,
        textAlign: "center",
        boxShadow: "0 6px 20px rgba(0,0,0,0.25)",
      }}
    >
      {message}
    </div>
  );
}

export default function R2PList() {
  // Next.js에서는 import.meta.env 대신 NEXT_PUBLIC_* 사용
  const base = process.env.NEXT_PUBLIC_API_BASE?.trim() || "http://thomasd.test";

  // 기본 필터
  const [owner, setOwner] = useState("tho1alice");
  const [role, setRole] = useState<Role>("payee");
  const [status, setStatus] = useState<Status>("paid");
  const [limit, setLimit] = useState(20);

  const {
    data,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    refetch,
    isFetching,
  } = useR2PInfinite(base, { owner, role, status: status || undefined, limit });

  // 안전한 items 전개 + 정제
  const items =
    (data?.pages ?? [])
      .flatMap((p: any) => (Array.isArray(p?.records) ? p.records : []))
      .filter((r: any) => r && typeof r === "object");

  // 무한 스크롤
  const sentinel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = sentinel.current;
    if (!el) return;
    const io = new IntersectionObserver((e) => {
      if (e[0].isIntersecting && hasNextPage && !isFetchingNextPage) {
        fetchNextPage();
      }
    });
    io.observe(el);
    return () => io.disconnect();
  }, [sentinel, hasNextPage, isFetchingNextPage, fetchNextPage]);

  // 필터 적용: queryKey가 바뀌므로 refetch만으로 첫 페이지부터 다시 가져옴
  function applyFilters(e?: React.FormEvent) {
    e?.preventDefault();
    window.scrollTo({ top: 0 });
    refetch();
  }

  // 토스트
  const [toast, setToast] = useState<string | null>(null);
  function showToast(msg: string) {
    setToast(msg);
    setTimeout(() => setToast(null), 1500);
  }

  async function refresh() {
    await refetch();
  }

  // 액션 핸들러 (id 없으면 조용히 무시)
  async function doApprove(id?: string) {
    if (!id) return showToast("missing id");
    try {
      await approveR2P(base, id);
      showToast("approved");
    } catch (e: any) {
      showToast(e?.message ?? "approve failed");
    } finally {
      refresh();
    }
  }
  async function doDecline(id?: string) {
    if (!id) return showToast("missing id");
    try {
      await declineR2P(base, id);
      showToast("declined");
    } catch (e: any) {
      showToast(e?.message ?? "decline failed");
    } finally {
      refresh();
    }
  }
  async function doCancel(id?: string) {
    if (!id) return showToast("missing id");
    try {
      await cancelR2P(base, id);
      showToast("canceled");
    } catch (e: any) {
      showToast(e?.message ?? "cancel failed");
    } finally {
      refresh();
    }
  }
  async function doCommit() {
    try {
      await commitRound(base);
      showToast("round committed");
    } catch (e: any) {
      showToast(e?.message ?? "commit failed");
    } finally {
      refresh();
    }
  }

  return (
    <div style={{ maxWidth: 840, margin: "0 auto", padding: 16 }}>
      <h2 style={{ marginBottom: 12 }}>Create / Quick Pay</h2>
      <R2PForm base={base} onDone={refresh} />

      <h2
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          marginTop: 16,
        }}
      >
        <span>R2P Records</span>
        <button onClick={doCommit} disabled={isFetching}>
          Commit Round
        </button>
      </h2>

      {/* 필터 바 */}
      <form
        onSubmit={applyFilters}
        style={{
          display: "grid",
          gridTemplateColumns: "1fr 140px 160px 100px auto",
          gap: 8,
          marginBottom: 16,
        }}
      >
        <input
          value={owner}
          onChange={(e) => setOwner(e.target.value)}
          placeholder="owner (addr or @alias)"
        />
        <select value={role} onChange={(e) => setRole(e.target.value as Role)}>
          <option value="payee">payee (from=owner)</option>
          <option value="payer">payer (to=owner)</option>
          <option value="any">any</option>
        </select>
        <select
          value={status}
          onChange={(e) => setStatus(e.target.value as Status)}
        >
          <option value="">(all statuses)</option>
          <option value="open">open</option>
          <option value="paid">paid</option>
          <option value="declined">declined</option>
          <option value="canceled">canceled</option>
        </select>
        <input
          type="number"
          min={1}
          max={200}
          value={limit}
          onChange={(e) => setLimit(Number(e.target.value) || 20)}
        />
        <button type="submit" disabled={isFetching}>
          {isFetching ? "Loading…" : "Apply"}
        </button>
      </form>

      {/* 리스트 */}
      {items.map((r: any, idx: number) => {
        const key =
          r.id ?? r.request_id ?? r.uuid ?? r._id ?? `row-${idx}`;
        const created = r.created_utc ?? r.created_at ?? r.created ?? "";
        const paid = r.paid_utc ?? r.paid_at ?? "";
        const declined = r.declined_utc ?? r.declined_at ?? "";
        const canceled = r.canceled_utc ?? r.canceled_at ?? "";
        const state = r.status ?? r.state ?? "";
        const canAct = state === "open" && !!r.id;

        return (
          <div
            key={key}
            style={{
              padding: 12,
              marginBottom: 8,
              border: "1px solid #eee",
              borderRadius: 12,
            }}
          >
            <div style={{ display: "flex", justifyContent: "space-between", gap: 8 }}>
              <div style={{ opacity: 0.6, fontSize: 12 }}>
                {r.id ?? r.request_id ?? "(no id)"}
              </div>
              <div style={{ fontSize: 12, opacity: 0.6 }}>
                {created && `created:${created}`}
                {paid && ` · paid:${paid}`}
                {declined && ` · declined:${declined}`}
                {canceled && ` · canceled:${canceled}`}
              </div>
            </div>

            <div style={{ fontWeight: 600 }}>
              {(r.from ?? "-")} → {(r.to ?? "-")}
            </div>

            <div
              style={{
                fontSize: 13,
                marginTop: 4,
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
              }}
            >
              <span>status: {state || "-"}</span>
              <div style={{ display: "flex", gap: 8 }}>
                <button disabled={!canAct} onClick={() => doApprove(r.id)}>
                  Approve
                </button>
                <button disabled={!canAct} onClick={() => doDecline(r.id)}>
                  Decline
                </button>
                <button disabled={!canAct} onClick={() => doCancel(r.id)}>
                  Cancel
                </button>
              </div>
            </div>
          </div>
        );
      })}

      {/* 무한 스크롤 센티넬 / 끝 표시 */}
      {!hasNextPage ? (
        <div style={{ padding: 24, textAlign: "center", opacity: 0.6 }}>
          end of list
        </div>
      ) : (
        <div ref={sentinel} style={{ padding: 24, textAlign: "center" }}>
          {isFetchingNextPage ? "Loading…" : " "}
        </div>
      )}

      {/* 토스트 */}
      <MiniToast message={toast} />
    </div>
  );
}
