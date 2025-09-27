export type R2PRecord = {
  id: string; from: string; to: string; status: string;
  created_utc: number; updated_utc?: number;
  paid_utc?: number; declined_utc?: number; canceled_utc?: number;
};
export type R2PPage = { ok: true; records: R2PRecord[]; next_cursor?: string };

export async function fetchR2PPage(base: string, p: {
  owner: string; role?: "payee"|"payer"|"any"; status?: "open"|"paid"|"declined"|"canceled";
  limit?: number; cursor?: string;
}): Promise<R2PPage> {
  const u = new URL("/r2p/list", base);
  u.searchParams.set("owner", p.owner);
  if (p.role) u.searchParams.set("role", p.role);
  if (p.status) u.searchParams.set("status", p.status);
  u.searchParams.set("limit", String(p.limit ?? 20));
  if (p.cursor) u.searchParams.set("cursor", p.cursor);
  const res = await fetch(u.toString());
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

