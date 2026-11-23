import { useEffect, useState, type FormEvent } from 'react';
import { Switch } from 'radix-ui';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ErrorBox } from '@/components/common';
import { api, ApiError, type Problem } from '@/lib/api';
import { emptyOwn, KINDS, ownBody, ownForm, searchChoices, withKind, type OwnForm, type OwnKind, type OwnModel, type OwnSearch } from '@/lib/own-model';
import { loadUser } from '@/lib/session';

// OwnModelSection is the reader's own model on the account page (ADR
// 0010): the form, tested before it is saved, and the switch to run on it.
export function OwnModelSection() {
  const [saved, setSaved] = useState<OwnModel | null | undefined>(undefined);
  const [f, setF] = useState<OwnForm>(emptyOwn);
  const [err, setErr] = useState('');
  const [problems, setProblems] = useState<Problem[]>([]);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api<OwnModel>('GET', '/api/me/own-model').then((m) => { setSaved(m); setF(ownForm(m)); }).catch((e) => {
      if (e instanceof ApiError && e.code === 'own_model_not_found') setSaved(null);
      else setErr(e instanceof ApiError ? e.message : '读取失败');
    });
  }, []);

  async function save(e: FormEvent) {
    e.preventDefault();
    setErr(''); setProblems([]);
    setBusy(true);
    try {
      const m = await api<OwnModel>('PUT', '/api/me/own-model', ownBody(f));
      setSaved(m); setF(ownForm(m));
      toast.success('测试通过, 已保存');
      void loadUser();
    } catch (ex) {
      setErr(ex instanceof ApiError ? ex.message : '保存失败');
      // the server's validation problems are plain strings
      setProblems(ex instanceof ApiError ? ex.problems.map((p) => (typeof p === 'string' ? { message: p } : p)) : []);
    } finally {
      setBusy(false);
    }
  }

  async function use(on: boolean) {
    try {
      setSaved(await api<OwnModel>('PUT', '/api/me/own-model/use', { on }));
      void loadUser();
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '切换失败');
    }
  }

  async function remove() {
    try {
      await api('DELETE', '/api/me/own-model');
      setSaved(null); setF(emptyOwn);
      toast.success('已删除自带模型');
      void loadUser();
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '删除失败');
    }
  }

  const set = <K extends keyof OwnForm>(k: K, v: OwnForm[K]) => setF((x) => ({ ...x, [k]: v }));
  const needsKey = !saved?.keyLast4;
  const needsTavily = f.search === 'tavily' && !saved?.tavilyLast4;
  return (
    <section className="me-section own-model" aria-labelledby="own-head">
      <h2 id="own-head">自带模型</h2>
      <p>打开后, 你的分析、探索对话和报告都用自己的模型, 不扣积分。出错时会告诉你是自带模型出错, 不会改用站点的模型。</p>
      {saved ? (
        <label className="own-switch">
          <Switch.Root className="switch" checked={saved.inUse} onCheckedChange={(v) => void use(v)} aria-label="使用自带模型">
            <Switch.Thumb className="switch-thumb" />
          </Switch.Root>
          <span><b>使用自带模型</b><small>{saved.model}{saved.search === 'off' ? ' · 未联网: 只查课程, 不搜索网页' : ''}</small></span>
        </label>
      ) : null}
      <form className="form-rows" onSubmit={save}>
        {err ? <ErrorBox message={err} problems={problems} /> : null}
        <div className="field">
          <label htmlFor="ok">接口</label>
          <select id="ok" className="select" value={f.kind} onChange={(e) => setF((x) => withKind(x, e.target.value as OwnKind))}>
            {KINDS.map((k) => <option key={k.id} value={k.id}>{k.label}</option>)}
          </select>
        </div>
        <div className="field">
          <label htmlFor="ou">地址{f.kind === 'deepseek' ? ' (已填好 DeepSeek 的)' : ''}</label>
          <Input id="ou" value={f.baseUrl} placeholder={f.kind === 'anthropic' ? 'https://api.anthropic.com' : 'https://…/v1'} onChange={(e) => set('baseUrl', e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="okey">key{saved?.keyLast4 ? ` (已保存, 末 4 位 ${saved.keyLast4}; 留空不变)` : ''}</label>
          <Input id="okey" type="password" autoComplete="off" value={f.key} onChange={(e) => set('key', e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="om">模型名</label>
          <Input id="om" value={f.model} onChange={(e) => set('model', e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="ocw">上下文长度 (token, 留空按 32000)</label>
          <Input id="ocw" inputMode="numeric" value={f.contextWindow} onChange={(e) => set('contextWindow', e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="os">联网搜索</label>
          <select id="os" className="select" value={f.search} onChange={(e) => set('search', e.target.value as OwnSearch)}>
            {searchChoices(f.kind).map((c) => <option key={c.id} value={c.id}>{c.label}</option>)}
          </select>
        </div>
        {f.search === 'tavily' ? (
          <div className="field">
            <label htmlFor="ot">Tavily key{saved?.tavilyLast4 ? ` (已保存, 末 4 位 ${saved.tavilyLast4}; 留空不变)` : ''}</label>
            <Input id="ot" type="password" autoComplete="off" value={f.tavilyKey} onChange={(e) => set('tavilyKey', e.target.value)} />
          </div>
        ) : null}
        <small className="own-note">
          key 用服务器的主密钥加密保存, 页面上只显示末 4 位, 管理员在管理页看不到。但请求是从服务器发出的, 运营服务器的人技术上仍能拿到 key。
          保存前会先测试: 模型做一次带工具的流式调用{f.search === 'tavily' ? ', Tavily key 做一次真实搜索' : ''}。
        </small>
        <div className="form-foot">
          {saved ? <Button type="button" variant="ghost" onClick={() => void remove()}>删除</Button> : null}
          <Button type="submit" variant="default" disabled={busy || !f.model.trim() || (needsKey && f.kind !== 'openai' && !f.key.trim()) || (needsTavily && !f.tavilyKey.trim())}>
            {busy ? '测试中…' : '测试并保存'}
          </Button>
        </div>
      </form>
    </section>
  );
}
