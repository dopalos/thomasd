import { useState } from "react";
import { createR2P, approveR2P, commitRound } from "../api/r2p";

export default function R2PForm({ base, onDone }: { base: string; onDone: () => void }) {
  const [fromAddr, setFromAddr] = useState("tho1alice");
  const [toAddr, setToAddr] = useState("tho1bob");
  const [amount, setAmount] = useState<number>(1234);
  const [memo, setMemo] = useState("demo");
  const [busy, setBusy] = useState(false);

  async function onCreate(e: React.FormEvent) {
    e.preventDefault();
    try {
      setBusy(true);
      const { id } = await createR2P(base, { from: fromAddr, to: toAddr, amount_mas: amount, memo });
      alert(`created: ${id}`);
      onDone();
    } catch (e:any) {
      alert(e.message || String(e));
    } finally { setBusy(false); }
  }

  async function onCreateApproveCommit(e: React.FormEvent) {
    e.preventDefault();
    try {
      setBusy(true);
      const { id } = await createR2P(base, { from: fromAddr, to: toAddr, amount_mas: amount, memo });
      await approveR2P(base, id);
      await commitRound(base);
      alert(`paid: ${id}`);
      onDone();
    } catch (e:any) {
      alert(e.message || String(e));
    } finally { setBusy(false); }
  }

  return (
    <form onSubmit={onCreate} style={{display:"grid", gridTemplateColumns:"1fr 1fr 120px 1fr auto", gap:8, marginBottom:16}}>
      <input value={fromAddr} onChange={e=>setFromAddr(e.target.value)} placeholder="from (addr)" />
      <input value={toAddr} onChange={e=>setToAddr(e.target.value)} placeholder="to (addr)" />
      <input type="number" min={1} value={amount} onChange={e=>setAmount(Number(e.target.value)||0)} placeholder="amount_mas" />
      <input value={memo} onChange={e=>setMemo(e.target.value)} placeholder="memo (opt)" />
      <div style={{display:"flex", gap:8}}>
        <button type="submit" disabled={busy}>Create</button>
        <button onClick={onCreateApproveCommit} disabled={busy} type="button">Create?묨pprove?묬ommit</button>
      </div>
    </form>
  );
}

