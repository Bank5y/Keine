import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ErrorBox } from '@/components/common';
import { api, ApiError } from '@/lib/api';
import { downloadLabel, embedState, megabytes, stillEmbedding, type EmbeddingProvider, type EmbeddingState } from '@/lib/embedding';
import { cn } from '@/lib/utils';

const TIERS: [EmbeddingProvider, string, string][] = [
  ['off', '关闭', '默认。召回只用概念和段落两路。'],
  ['external', '外部', 'OpenAI 兼容的嵌入 API, 用量记在站点上。'],
  ['local', '本地', '在服务器上运行小模型, 不出网; 模型由管理员下载。'],
];

// how often the page looks again while something is being embedded
const FOLLOW_MS = 2000;

// AdminEmbedding is the 嵌入 view of the models tab (#227): one site-wide
// setting, off, an external API or a local model, and how far each
// course is embedded.
export function AdminEmbedding() {
  const [st, setSt] = useState<EmbeddingState | null>(null);
  const [err, setErr] = useState('');
  const [tier, setTier] = useState<EmbeddingProvider | null>(null);
  const load = useCallback(() => api<EmbeddingState>('GET', '/api/admin/embedding').then((s) => { setSt(s); setErr(''); }).catch((e) => setErr(e.message)), []);
  useEffect(() => { void load(); }, [load]);
  const following = !!st && stillEmbedding(st);
  useEffect(() => {
    if (!following) return;
    const t = setInterval(() => void load(), FOLLOW_MS);
    return () => clearInterval(t);
  }, [following, load]);

  if (err) return <ErrorBox message={err} />;
  if (!st) return <p className="usage-note">正在加载…</p>;
  const s = st.settings;
  const shown = tier ?? s.provider;

  async function save(method: string, url: string, body: unknown, done: string) {
    try {
      setSt(await api<EmbeddingState>(method, url, body));
      setTier(null);
      toast.success(done);
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '保存失败');
    }
  }

  return (
    <div className="embedding">
      <p className="tab-intro">嵌入给概念索引的召回加上向量这一路: 字面不同、意思相近的段落也能找到。全站一套设置; 换提供方或模型后, 已有向量全部失效, 各课程重新嵌入。</p>
      <div className="tiers" role="radiogroup" aria-label="嵌入">
        {TIERS.map(([t, name, desc]) => (
          <button key={t} type="button" role="radio" aria-checked={shown === t} className={cn('tier', shown === t && 'on')} onClick={() => setTier(t)}>
            <span className="dot" aria-hidden /><b>{name}</b>{s.provider === t ? <span className="st ok">在用</span> : null}<small>{desc}</small>
          </button>
        ))}
      </div>
      {shown === 'off' && s.provider !== 'off' ? (
        <div className="tier-form"><Button variant="default" onClick={() => void save('PUT', '/api/admin/embedding/off', undefined, '已关闭嵌入')}>关闭嵌入</Button></div>
      ) : null}
      {shown === 'external' ? <ExternalForm st={st} save={save} /> : null}
      {shown === 'local' ? <LocalForm st={st} reload={load} save={save} /> : null}
      {s.provider !== 'off' ? (
        <>
          <h3 className="usage-h">各课程的向量</h3>
          <div className="ledger embed-courses">
            {st.courses.map((c) => {
              const e = embedState(c);
              return (
                <div className="ledger-row" key={c.courseId}>
                  <span className="name">{c.title}</span>
                  <span className={cn('st', e.tone)}>{e.label}</span>
                  <span className="meta">{c.done} / {c.total} 段</span>
                </div>
              );
            })}
          </div>
          <p className="meta embed-foot">当前在用  {s.provider === 'external' ? `外部 ${s.model}` : `本地 ${s.localModel}`}  ·  {s.dims} 维</p>
        </>
      ) : null}
    </div>
  );
}

type Save = (method: string, url: string, body: unknown, done: string) => Promise<void>;

function ExternalForm({ st, save }: { st: EmbeddingState; save: Save }) {
  const s = st.settings;
  const [baseUrl, setBaseUrl] = useState(s.baseUrl);
  const [key, setKey] = useState('');
  const [model, setModel] = useState(s.model);
  const [rate, setRate] = useState(String(s.rate));
  const [busy, setBusy] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    await save('PUT', '/api/admin/embedding/external', { baseUrl, key, model, rate: Number(rate) || 0 }, '已切到外部嵌入');
    setBusy(false);
  }
  if (!s.canStoreKeys) return <ErrorBox message="部署没有配置主密钥 (KEINE_MASTER_KEY), 不能保存嵌入 API 的 Key。" />;
  return (
    <form className="tier-form" onSubmit={submit}>
      <div className="field"><label htmlFor="eb">Base URL</label><Input id="eb" placeholder="https://api.example.com/v1" value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} /></div>
      <div className="field"><label htmlFor="ek">Key</label><Input id="ek" type="password" placeholder={s.keyLast4 ? `····${s.keyLast4} (不改就留空)` : ''} value={key} onChange={(e) => setKey(e.target.value)} /></div>
      <div className="field-pair">
        <div className="field"><label htmlFor="em">模型名</label><Input id="em" value={model} onChange={(e) => setModel(e.target.value)} /></div>
        <div className="field"><label htmlFor="er">费率</label><div className="rate-row"><Input id="er" type="number" min={0} step="any" value={rate} onChange={(e) => setRate(e.target.value)} /><span className="hint">积分 / 百万 token</span></div></div>
      </div>
      <div className="tier-acts">
        <Button type="submit" variant="default" disabled={busy || !baseUrl.trim() || !model.trim() || (!key && !s.keyLast4)}>{busy ? '正在测试…' : '测试并保存'}</Button>
        <span className="hint">保存前先嵌入一句话试试; 连不上就不保存, 原来的设置不变。</span>
      </div>
    </form>
  );
}

function LocalForm({ st, reload, save }: { st: EmbeddingState; reload: () => void; save: Save }) {
  const s = st.settings;
  const [model, setModel] = useState(s.localModel || s.models[0]?.id || '');
  const [mirror, setMirror] = useState(s.mirror);
  const d = s.downloads.find((x) => x.model === model);
  const m = s.models.find((x) => x.id === model);
  async function download() {
    try {
      await api('POST', '/api/admin/embedding/download', { model, mirror });
      reload();
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '下载失败');
    }
  }
  return (
    <div className="tier-form">
      <div className="field"><label htmlFor="lm">模型</label>
        <select id="lm" className="select" value={model} onChange={(e) => setModel(e.target.value)}>
          {s.models.map((x) => <option key={x.id} value={x.id}>{x.name}  ·  {x.dims} 维  ·  约 {Math.round(x.size / 1e6)} MB</option>)}
        </select>
      </div>
      <div className="field"><label htmlFor="ls">下载源</label><Input id="ls" value={mirror} onChange={(e) => setMirror(e.target.value)} /><div className="hint">Hugging Face 或它的镜像, 比如 https://hf-mirror.com。</div></div>
      {d && d.status !== 'done' ? (
        <div className="download">
          <div className="stage-head"><b className={cn(d.status === 'failed' ? 'st off' : 'st warn')}>{d.status === 'failed' ? '下载失败' : '下载中'}</b><span className="num">{downloadLabel(d)}</span></div>
          <div className="stage-bar running"><span style={{ width: `${d.total ? Math.min(100, (d.done / d.total) * 100) : 0}%` }} /></div>
          {d.error ? <p className="hint">{d.error}</p> : null}
        </div>
      ) : null}
      <div className="tier-acts">
        {d?.status === 'done' ? (
          <Button variant="default" disabled={s.provider === 'local' && s.localModel === model} onClick={() => void save('PUT', '/api/admin/embedding/local', { model }, '已切到本地嵌入')}>{s.provider === 'local' && s.localModel === model ? '在用' : '使用这个模型'}</Button>
        ) : (
          <Button variant="default" disabled={d?.status === 'running'} onClick={() => void download()}>{d?.status === 'failed' ? '重新下载' : d?.status === 'running' ? '下载中…' : `下载 (${m ? megabytes(m.size) : ''} MB)`}</Button>
        )}
        <span className="hint">下载完、试着嵌入一句话成功后才切到本地档; 断网后重新下载会从断点接着下。</span>
      </div>
    </div>
  );
}
