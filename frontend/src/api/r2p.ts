// frontend/src/api/r2p.ts

export type R2PRecord = {
  id: string; from: string; to: string; status: string;
  created_utc: number; updated_utc?: number;
  paid_utc?: number; declined_utc?: number; canceled_utc?: number;
};

export type R2PPage = { ok: true; records: R2PRecord[]; next_cursor?: string };

// ---- 페이지 조회 (단 한 번만 정의) ----
export async function fetchR2PPage(base: string, p: {
  owner: string; role?: "payee"|"payer"|"any"; status?: "open"|"paid"|"declined"|"canceled";
  limit?: number; cursor?: string;
}): Promise<R2PPage> {
  const u = new URL("/r2p/list", base);
  u.searchParams.set("owner", p.owner);
  if (p.role)   u.searchParams.set("role", p.role);
  if (p.status) u.searchParams.set("status", p.status);
  u.searchParams.set("limit", String(p.limit ?? 20));
  if (p.cursor) u.searchParams.set("cursor", p.cursor);

  const res = await fetch(u.toString());
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

// ---- 공통 POST 헬퍼 ----
async function postJSON<T>(url: string, body: any): Promise<T> {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body ?? {}),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

// ---- 변이 API들 ----
export async function createR2P(base: string, b: {
  from: string; to: string; amount_mas: number; memo?: string;
}): Promise<{ id: string }> {
  return postJSON<{ id: string }>(new URL("/r2p/create", base).toString(), b);
}

export async function approveR2P(base: string, id: string): Promise<{ ok: true }> {
  return postJSON<{ ok: true }>(new URL("/r2p/approve", base).toString(), { id });
}

export async function declineR2P(base: string, id: string): Promise<{ ok: true }> {
  return postJSON<{ ok: true }>(new URL("/r2p/decline", base).toString(), { id });
}

export async function cancelR2P(base: string, id: string): Promise<{ ok: true }> {
  return postJSON<{ ok: true }>(new URL("/r2p/cancel", base).toString(), { id });
}

export async function commitRound(base: string): Promise<{ ok: true } | any> {
  return postJSON(new URL("/round/commit_safe2", base).toString(), {});
}

