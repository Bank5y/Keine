import { useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { ErrorBox } from '@/components/common';
import { api, ApiError } from '@/lib/api';
import { quota, type Plan } from '@/lib/credits';

const failed = (ex: unknown) => toast.error(ex instanceof ApiError ? ex.message : '操作失败');

// why a plan cannot be deleted, if it cannot
function undeletable(p: Plan) {
  if (p.builtin) return '内置套餐不能删除';
  if (p.default) return '默认套餐不能删除';
  if (p.users > 0) return '还有用户在这个套餐里, 先在「用户」里把他们改到别的套餐';
  return '';
}

// AdminPlans lists the plans (#130): each reader is on one, whose daily
// credits reset at midnight; new readers start on the default plan.
export function AdminPlans() {
  const [plans, setPlans] = useState<Plan[] | null>(null);
  const [err, setErr] = useState('');
  const [editing, setEditing] = useState<Plan | 'new' | null>(null);
  const [deleting, setDeleting] = useState<Plan | null>(null);

  const load = () => api<{ plans: Plan[] }>('GET', '/api/admin/plans').then((r) => setPlans(r.plans)).catch((e) => setErr(e.message));
  useEffect(() => { void load(); }, []);

  async function makeDefault(p: Plan) {
    try {
      await api('PUT', `/api/admin/plans/${p.id}/default`);
      toast.success(`新用户会进入「${p.name}」`);
      void load();
    } catch (ex) { failed(ex); }
  }

  async function remove(p: Plan) {
    try {
      await api('DELETE', `/api/admin/plans/${p.id}`);
      toast.success(`已删除「${p.name}」`);
      setDeleting(null);
      void load();
    } catch (ex) { failed(ex); }
  }

  return (
    <>
      <p className="tab-intro">每个用户属于一个套餐, 每天 0 点重置成套餐的每日额度, 不结转。新用户进入默认套餐。改套餐即时生效。</p>
      <div className="result-bar" style={{ marginTop: 0 }}>
        <span className="count">{plans ? <><b>{plans.length}</b>个套餐</> : '正在加载…'}</span>
        <Button size="sm" variant="default" onClick={() => setEditing('new')}>添加套餐</Button>
      </div>
      {err ? <ErrorBox message={err} /> : null}
      <div className="ledger plans">
        {(plans || []).map((p) => {
          const why = undeletable(p);
          return (
            <div className="ledger-row" key={p.id}>
              <span className="plan-name">
                <span className="name">{p.name}{p.default ? <span className="tag">默认</span> : null}</span>
                {p.default ? <small>新用户自动进入</small> : p.builtin ? <small>内置 · 不计额度, 照样记用量</small> : null}
              </span>
              <span className="quota">{p.daily === null ? '不限' : `${quota(p.daily)} 积分 / 天`}</span>
              <span className="meta">{p.users} 人</span>
              <span className="acts">
                {!p.default && !p.builtin ? <Button size="sm" variant="ghost" onClick={() => void makeDefault(p)}>设为默认</Button> : null}
                <Button size="sm" variant="ghost" onClick={() => setEditing(p)}>编辑</Button>
                <Button size="sm" variant="ghost" disabled={!!why} title={why || undefined} onClick={() => setDeleting(p)}>删除</Button>
              </span>
            </div>
          );
        })}
      </div>
      <PlanDialog plan={editing} onClose={() => setEditing(null)} onDone={() => { setEditing(null); void load(); }} />
      <Dialog open={!!deleting} onOpenChange={(v) => { if (!v) setDeleting(null); }} title="删除套餐">
        <p>删除「<b>{deleting?.name}</b>」？</p>
        <div className="head-actions">
          <Button onClick={() => setDeleting(null)}>取消</Button>
          <Button variant="default" onClick={() => deleting && void remove(deleting)}>删除</Button>
        </div>
      </Dialog>
    </>
  );
}

// PlanDialog adds a plan, or edits one; an edit applies at once to
// everyone on it.
function PlanDialog({ plan, onClose, onDone }: { plan: Plan | 'new' | null; onClose: () => void; onDone: () => void }) {
  const [name, setName] = useState('');
  const [daily, setDaily] = useState('');
  const [unlimited, setUnlimited] = useState(false);
  const [err, setErr] = useState('');
  const editing = plan && plan !== 'new' ? plan : null;

  useEffect(() => {
    if (!plan) return;
    const p = plan === 'new' ? null : plan;
    setErr('');
    setName(p?.name ?? '');
    setUnlimited(p ? p.daily === null : false);
    setDaily(p?.daily != null ? String(p.daily) : '');
  }, [plan]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    const body = { name: name.trim(), daily: unlimited ? null : Number(daily) };
    try {
      await api(editing ? 'PUT' : 'POST', editing ? `/api/admin/plans/${editing.id}` : '/api/admin/plans', body);
      toast.success(editing ? `已保存「${body.name}」` : `已添加「${body.name}」`);
      onDone();
    } catch (ex) {
      setErr(ex instanceof ApiError ? ex.message : '保存失败');
    }
  }

  const valid = name.trim() && (unlimited || (daily.trim() !== '' && Number(daily) >= 0));
  return (
    <Dialog open={!!plan} onOpenChange={(v) => { if (!v) onClose(); }} title={editing ? `编辑套餐「${editing.name}」` : '添加套餐'}>
      <form onSubmit={submit} className="mt-4">
        {editing ? <p>改动即时生效: 今天已用的积分不变, 剩余按新额度计算。</p> : null}
        {err ? <ErrorBox message={err} /> : null}
        <div className="field"><label htmlFor="pn">名称</label><Input id="pn" autoFocus value={name} onChange={(e) => setName(e.target.value)} /></div>
        <div className="field">
          <label htmlFor="pd">每日额度 (积分)</label>
          <div className="quota-row">
            <Input id="pd" type="number" min={0} step="any" inputMode="decimal" disabled={unlimited} value={unlimited ? '' : daily} onChange={(e) => setDaily(e.target.value)} />
            <label className="check"><input type="checkbox" checked={unlimited} onChange={(e) => setUnlimited(e.target.checked)} />不限</label>
          </div>
          <div className="hint">填 0 时这个套餐的用户不能用 AI。</div>
        </div>
        <div className="head-actions">
          <Button onClick={onClose}>取消</Button>
          <Button type="submit" variant="default" disabled={!valid}>保存</Button>
        </div>
      </form>
    </Dialog>
  );
}
