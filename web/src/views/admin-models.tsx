import { useEffect, useState, type FormEvent } from 'react';
import { Switch } from 'radix-ui';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Empty, ErrorBox } from '@/components/common';
import { api, ApiError, type Problem } from '@/lib/api';
import { chooseFormat, chooseProvider, keepRoute, routeChoice, searchLabel, searchRoutes, type Format, type Preset, type SearchRoute } from '@/lib/provider';
import { problemText, TURN_NAMES, type AIProblem, type TurnKind } from '@/lib/ai';
import { Link } from '@/lib/router';
import { cn } from '@/lib/utils';

interface Rates { input: number; cacheRead: number; cacheWrite: number; output: number; search: number }
interface SiteModel {
  id: number; format: string; provider: string; baseUrl: string; model: string; displayName: string;
  contextWindow: number | null; maxOutput: number | null; rates: Rates | null; keyLast4: string;
  searchRoute: SearchRoute; search: SearchRoute; searchReady: boolean;
}
interface TurnModel { modelId: number; thinking: boolean }
interface State {
  models: SiteModel[]; turns: Record<TurnKind, TurnModel | null>; aiAvailable: boolean; problem: AIProblem | null; masterKey: boolean; providers: Preset[]; formats: Format[];
  tavily: { set: boolean; keyLast4: string };
}

const TURNS: [TurnKind, string, string][] = [
  ['analysis', TURN_NAMES.analysis, '读者选中原文点「分析」'],
  ['followup', TURN_NAMES.followup, '在探索对话里接着问'],
  ['report', TURN_NAMES.report, '在探索对话里生成探索报告'],
  ['concept_index', TURN_NAMES.concept_index, '为课程版本建立概念索引'],
];
const RATE_FIELDS: [keyof Rates, string][] = [['input', '输入'], ['cacheRead', '缓存命中'], ['cacheWrite', '缓存写入'], ['output', '输出'], ['search', '搜索']];

const contextLabel = (m: SiteModel) => `${Math.round((m.contextWindow ?? 128000) / 1000)}k 上下文`;

// the server's validation problems are plain strings
const problemsOf = (ex: ApiError): Problem[] => ex.problems.map((p) => (typeof p === 'string' ? { message: p } : p));

// the three views of the models tab, each at its own address
const VIEWS: ['models' | 'turns' | 'web', string, string][] = [['models', '/admin/models', '模型'], ['turns', '/admin/models/turns', '轮次'], ['web', '/admin/models/web', '联网']];

export function AdminModels({ path }: { path: string }) {
  const [st, setSt] = useState<State | null>(null);
  const [err, setErr] = useState('');
  const [editing, setEditing] = useState<SiteModel | 'new' | null>(null);
  const [deleting, setDeleting] = useState<SiteModel | null>(null);

  const load = () => api<State>('GET', '/api/admin/models').then(setSt).catch((e) => setErr(e.message));
  useEffect(() => { void load(); }, []);

  async function setTurn(kind: TurnKind, modelId: number | null, thinking: boolean) {
    try {
      setSt(await api<State>('PUT', `/api/admin/turns/${kind}`, { modelId, thinking }));
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '保存失败');
    }
  }

  async function remove(m: SiteModel) {
    try {
      await api('DELETE', `/api/admin/models/${m.id}`);
      toast.success(`已删除 ${m.displayName}`);
      setDeleting(null);
      void load();
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '删除失败');
    }
  }


  const view = path.startsWith('/admin/models/turns') ? 'turns' : path.startsWith('/admin/models/web') ? 'web' : 'models';
  return (
    <>
      <nav className="seg models-seg" aria-label="模型设置">
        {VIEWS.map(([v, href, label]) => (
          <Link key={v} href={href} keepScroll className={cn(view === v && 'on')} aria-current={view === v ? 'page' : undefined}>{label}</Link>
        ))}
      </nav>
      {err ? <ErrorBox message={err} /> : null}
      {view === 'models' ? (
        <>
          <p className="tab-intro">分析、探索对话、探索报告和概念索引都用这里的模型。改费率只影响之后的运行, 已记下的用量不变。</p>
          <div className="result-bar" style={{ marginTop: 0, marginBottom: 14 }}>
            <span className="count">{st ? <><b>{st.models.length}</b>个模型</> : '正在加载…'}</span>
            <Button size="sm" variant="default" onClick={() => setEditing('new')}>添加模型</Button>
          </div>
          {st && !st.models.length ? <Empty title="还没有模型">添加一个 OpenAI 兼容或 Anthropic Messages 格式的模型, 再给四种轮次选上它。</Empty> : null}
          {st?.models.length ? (
            <div className="ledger">
              {st.models.map((m) => (
                <div className="model-row" key={m.id}>
                  <div className="min-w-0">
                    <div className="name">{m.displayName}</div>
                    <div className="meta">{m.model}  ·  {st.providers.find((p) => p.id === m.provider)?.name ?? st.formats.find((x) => x.id === m.format)?.name ?? m.format}  ·  {contextLabel(m)}</div>
                    <div className={cn('meta', !m.searchReady && 'st warn')}>联网: {searchLabel(m)}</div>
                  </div>
                  {m.rates ? (
                    <div className="meta rates">
                      <div className="text-ink-2">输入 {m.rates.input} · 命中 {m.rates.cacheRead} · 写入 {m.rates.cacheWrite} · 输出 {m.rates.output}</div>
                      <div>每百万 token · 搜索 {m.rates.search} / 次</div>
                    </div>
                  ) : (
                    <div className="rates"><span className="st warn">未配费率</span><div className="mt-1 text-xs text-ink-3">不能被轮次类型选用</div></div>
                  )}
                  <span className="meta key">{m.keyLast4 ? `Key ····${m.keyLast4}` : '没有 Key'}</span>
                  <span className="acts">
                    <Button size="sm" variant="ghost" onClick={() => setEditing(m)}>编辑</Button>
                    <Button size="sm" variant="ghost" onClick={() => setDeleting(m)}>删除</Button>
                  </span>
                </div>
              ))}
            </div>
          ) : null}
        </>
      ) : null}
      {view === 'turns' && st ? (
        <>
          <p className="tab-intro">四种轮次都选好模型后, AI 功能才对所有人出现。没配费率的模型不能选。</p>
          <div className="mb-4 flex flex-wrap items-baseline gap-x-5 gap-y-1">
            <span className={cn('st', st.aiAvailable ? 'ok' : 'warn')} style={{ fontSize: 13, fontWeight: 600 }}>{st.aiAvailable ? 'AI 已启用' : 'AI 未启用'}</span>
            <span className="text-[13px] text-ink-3">{st.aiAvailable ? '读者能在阅读页使用分析和探索' : problemText(st.problem) ?? '在这里检查配置。'}</span>
          </div>
          <div className="ledger">
            {TURNS.map(([kind, label, desc]) => {
              const t = st.turns[kind];
              return (
                <div className="turn-row" key={kind}>
                  <div><div className="text-sm font-semibold">{label}</div><div className="text-xs text-ink-3">{desc}</div></div>
                  <select
                    className="select" aria-label={`${label}用的模型`} value={t?.modelId ?? ''}
                    onChange={(e) => setTurn(kind, e.target.value ? Number(e.target.value) : null, t?.thinking ?? false)}
                  >
                    <option value="">未选择</option>
                    {st.models.map((m) => <option key={m.id} value={m.id} disabled={!m.rates}>{m.displayName}{m.rates ? '' : '（未配费率, 不能选用）'}</option>)}
                  </select>
                  <label className="flex items-center gap-2.5 text-[13px]">
                    <Switch.Root className="switch" checked={t?.thinking ?? false} disabled={!t} onCheckedChange={(v) => t && setTurn(kind, t.modelId, v)}>
                      <Switch.Thumb className="switch-thumb" />
                    </Switch.Root>
                    思考
                  </label>
                </div>
              );
            })}
          </div>
        </>
      ) : null}
      {view === 'web' && st ? (
        <>
          <TavilyKey state={st.tavily} masterKey={st.masterKey} onSaved={setSt} />
          <h3 className="usage-h">各模型的联网路线</h3>
          {st.models.length ? (
            <div className="usage-table-wrap">
              <table className="usage-table routes-table">
                <thead><tr><th scope="col">模型</th><th scope="col">设置</th><th scope="col">实际路线</th><th scope="col">状态</th></tr></thead>
                <tbody>
                  {st.models.map((m) => (
                    <tr key={m.id}>
                      <td className="who">{m.displayName}</td>
                      <td className="plan">{routeChoice(m.searchRoute)}</td>
                      <td className="plan">{m.searchReady ? searchLabel(m) : '—'}</td>
                      <td className="plan">{m.searchReady
                        ? <span className="st ok">可用</span>
                        : <span className="st warn">没有可用路线: 配置 Tavily key, 或在模型里选「不联网」</span>}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : <Empty title="还没有模型" />}
          <div className="server-note">
            <b>抓取网页从服务器发出</b>
            <p>模型需要读网页时, 由 Keine 的服务器主动访问外部网站, 而不是读者的浏览器。</p>
          </div>
        </>
      ) : null}
      <ModelDialog
        target={editing} masterKey={st?.masterKey ?? false} presets={st?.providers ?? []} formats={st?.formats ?? []}
        onClose={() => setEditing(null)} onDone={() => { setEditing(null); void load(); }}
      />
      <Dialog open={!!deleting} onOpenChange={(v) => { if (!v) setDeleting(null); }} title="删除模型">
        <p>删除 <b>{deleting?.displayName}</b> 后不能恢复。正用于轮次类型的模型要先换掉。</p>
        <div className="head-actions">
          <Button onClick={() => setDeleting(null)}>取消</Button>
          <Button variant="default" onClick={() => deleting && remove(deleting)}>删除</Button>
        </div>
      </Dialog>
    </>
  );
}

interface Form { format: string; provider: string; displayName: string; model: string; baseUrl: string; key: string; contextWindow: string; maxOutput: string; rates: Record<keyof Rates, string>; searchRoute: SearchRoute }

const blank: Form = { format: 'openai', provider: '', displayName: '', model: '', baseUrl: '', key: '', contextWindow: '', maxOutput: '', rates: { input: '', cacheRead: '', cacheWrite: '', output: '', search: '' }, searchRoute: '' };

function formOf(m: SiteModel): Form {
  const r = m.rates;
  return {
    format: m.format, provider: m.provider, displayName: m.displayName, model: m.model, baseUrl: m.baseUrl, key: '',
    contextWindow: m.contextWindow ? String(m.contextWindow) : '', maxOutput: m.maxOutput ? String(m.maxOutput) : '', searchRoute: m.searchRoute,
    rates: r ? { input: String(r.input), cacheRead: String(r.cacheRead), cacheWrite: String(r.cacheWrite), output: String(r.output), search: String(r.search) } : blank.rates,
  };
}

// payload turns the form into the API body; the server validates it.
function payload(f: Form) {
  const rateVals = RATE_FIELDS.map(([k]) => f.rates[k].trim());
  const rates = rateVals.every((v) => v === '') ? null : Object.fromEntries(RATE_FIELDS.map(([k], i) => [k, rateVals[i] === '' ? null : Number(rateVals[i])]));
  const cw = f.contextWindow.trim();
  const mo = f.maxOutput.trim();
  return {
    format: f.format, provider: f.provider, displayName: f.displayName, model: f.model, baseUrl: f.baseUrl, key: f.key,
    contextWindow: cw === '' ? null : Number(cw), maxOutput: mo === '' ? null : Number(mo), rates, searchRoute: f.searchRoute,
  };
}

function ModelDialog({ target, masterKey, presets, formats, onClose, onDone }: { target: SiteModel | 'new' | null; masterKey: boolean; presets: Preset[]; formats: Format[]; onClose: () => void; onDone: () => void }) {
  const [f, setF] = useState<Form>(blank);
  const [err, setErr] = useState<{ message: string; problems?: Problem[]; test?: boolean } | null>(null);
  const [busy, setBusy] = useState(false);
  const editing = target && target !== 'new' ? target : null;
  const preset = presets.find((p) => p.id === f.provider);
  useEffect(() => {
    if (!target) return;
    setF(target === 'new' ? blank : formOf(target));
    setErr(null);
    setBusy(false);
  }, [target]);

  const set = (k: keyof Omit<Form, 'rates'>) => (e: { target: { value: string } }) => setF((x) => ({ ...x, [k]: e.target.value }));

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      if (editing) await api('PUT', `/api/admin/models/${editing.id}`, payload(f));
      else await api('POST', '/api/admin/models', payload(f));
      toast.success(`已保存 ${f.displayName.trim()}`);
      onDone();
    } catch (ex) {
      if (ex instanceof ApiError) setErr({ message: ex.message, problems: problemsOf(ex), test: ex.code === 'model_test_failed' });
      else setErr({ message: '保存失败' });
      setBusy(false);
    }
  }

  return (
    <Dialog open={!!target} onOpenChange={(v) => { if (!v && !busy) onClose(); }} title={editing ? `编辑「${editing.displayName}」` : '添加模型'} wide>
      <form onSubmit={submit} className="model-form">
        <p>保存前会发一次带假工具的流式请求, 通过了才保存。</p>
        <div className="grid grid-cols-1 gap-x-4 sm:grid-cols-2">
          <div className="field"><label htmlFor="mf">接口格式</label>
            <select id="mf" className="select" value={f.format} onChange={(e) => setF((x) => chooseFormat(x, e.target.value, formats, presets))}>
              {formats.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
            </select>
          </div>
          <div className="field"><label htmlFor="mp">服务商配置</label>
            <select id="mp" className="select" value={f.provider} onChange={(e) => setF((x) => ({ ...chooseProvider(x, e.target.value, presets), searchRoute: keepRoute(x.searchRoute, x.format, e.target.value) }))}>
              <option value="">无</option>
              {presets.filter((p) => p.format === f.format).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
          </div>
        </div>
        {preset ? <span className="hint provider-hint">已套用 {preset.name} 的默认地址和显示名称，填 key 和模型名即可。</span> : null}
        <div className="grid grid-cols-1 gap-x-4 sm:grid-cols-2">
          <div className="field"><label htmlFor="mn">显示名称</label><Input id="mn" placeholder="例如: DeepSeek Flash" value={f.displayName} onChange={set('displayName')} /></div>
          <div className="field"><label htmlFor="mm">模型名</label><Input id="mm" className="font-mono" placeholder={preset ? '例如: deepseek-flash' : f.format === 'anthropic' ? '例如: claude-opus-5-5' : '例如: deepseek-chat'} value={f.model} onChange={set('model')} /></div>
        </div>
        <div className="field"><label htmlFor="mu">地址</label><Input id="mu" className="font-mono" placeholder={f.format === 'anthropic' ? 'https://api.anthropic.com' : 'https://api.example.com/v1'} value={f.baseUrl} onChange={set('baseUrl')} /></div>
        <div className="field"><label htmlFor="mk">Key</label>
          <Input
            id="mk" type="password" autoComplete="off" disabled={!masterKey} value={f.key} onChange={set('key')}
            placeholder={!masterKey ? '不能保存 key' : editing?.keyLast4 ? `已保存 ····${editing.keyLast4}, 留空则不改` : preset ? `粘贴 ${preset.name} 的 API key` : '没有认证的端点可以留空'}
          />
          {masterKey
            ? <span className="hint">加密存库, 保存后只显示末 4 位。</span>
            : <span className="hint warn">服务器没有配置加密主密钥 (KEINE_MASTER_KEY), 配好并重启后才能保存 key。</span>}
        </div>
        <div className="grid grid-cols-1 gap-x-4 sm:grid-cols-2">
          <div className="field"><label htmlFor="mc">上下文窗口 (token)</label><Input id="mc" inputMode="numeric" placeholder="128000" value={f.contextWindow} onChange={set('contextWindow')} /><span className="hint">留空按 128k。</span></div>
          <div className="field"><label htmlFor="mo">最大输出 (token)</label><Input id="mo" inputMode="numeric" placeholder="8192" value={f.maxOutput} onChange={set('maxOutput')} /><span className="hint">概念索引和报告每次调用的上限, 写满了会续写。留空按 8192 (开思考 12288)。</span></div>
          <div className="field"><label htmlFor="ms">联网搜索</label>
            <select id="ms" className="select" value={f.searchRoute} onChange={(e) => setF((x) => ({ ...x, searchRoute: e.target.value as SearchRoute }))}>
              {searchRoutes(f.format, f.provider).map((r) => <option key={r.id} value={r.id}>{r.label}</option>)}
            </select>
            <span className="hint">是否搜索由模型决定。解析不出路线时 AI 不启用。</span>
          </div>
        </div>
        <fieldset className="field">
          <legend>费率 (积分)</legend>
          <div className="rate-grid">
            {RATE_FIELDS.map(([k, label]) => (
              <label key={k}><span>{label}</span>
                <Input inputMode="decimal" className="font-mono" value={f.rates[k]} onChange={(e) => setF((x) => ({ ...x, rates: { ...x.rates, [k]: e.target.value } }))} />
              </label>
            ))}
          </div>
          <span className="hint">前四项每百万 token, 搜索每次; 可以填 0。五项都留空表示未配费率, 这个模型就不能被轮次类型选用。</span>
        </fieldset>
        {err ? <ErrorBox message={err.test ? `${err.message}。没有保存。` : err.message} problems={err.problems} /> : null}
        <div className="head-actions">
          <Button onClick={onClose} disabled={busy}>取消</Button>
          <Button type="submit" variant="default" disabled={busy || !f.displayName.trim() || !f.model.trim() || !f.baseUrl.trim()}>{busy ? '测试中…' : '测试并保存'}</Button>
        </div>
      </form>
    </Dialog>
  );
}

// TavilyKey is the site's Tavily key, which models without a search
// service of their own search with. Saving it runs one real search.
function TavilyKey({ state, masterKey, onSaved }: { state: { set: boolean; keyLast4: string }; masterKey: boolean; onSaved: (s: State) => void }) {
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  async function run(method: 'PUT' | 'DELETE') {
    setBusy(true);
    setErr('');
    try {
      onSaved(await api<State>(method, '/api/admin/tavily', method === 'PUT' ? { key } : undefined));
      setKey('');
      toast.success(method === 'PUT' ? '已保存 Tavily key' : '已删除 Tavily key');
    } catch (ex) {
      setErr(ex instanceof ApiError ? ex.message : '保存失败');
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="tavily-key" onSubmit={(e) => { e.preventDefault(); void run('PUT'); }}>
      <div className="field"><label htmlFor="tk">站点 Tavily key</label>
        <div className="flex flex-wrap gap-2">
          <Input
            id="tk" type="password" autoComplete="off" className="min-w-0 flex-1" disabled={!masterKey || busy} value={key} onChange={(e) => setKey(e.target.value)}
            placeholder={!masterKey ? '不能保存 key' : state.set ? `已保存 ····${state.keyLast4}, 填新的替换` : 'tvly-…'}
          />
          <Button type="submit" size="sm" variant="default" disabled={busy || !key.trim()}>{busy ? '测试中…' : '测试并保存'}</Button>
          {state.set ? <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => void run('DELETE')}>删除</Button> : null}
        </div>
        <span className="hint">没有自带搜索的模型 (DeepSeek 和 Anthropic 以外) 用它搜索 (只搜索, 不抓取网页)。保存前会做一次真实搜索。</span>
      </div>
      {err ? <ErrorBox message={err} /> : null}
    </form>
  );
}
