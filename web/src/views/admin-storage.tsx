import { useEffect, useState, type ReactNode } from 'react';
import { Switch } from 'radix-ui';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Confirm } from '@/components/confirm';
import { ErrorBox } from '@/components/common';
import { api, ApiError } from '@/lib/api';
import {
  DELIVERY, DIRECTIONS, formOf, MEDIA, migrationLine, payload, progressOf, STEP_NAMES,
  type Direction, type Migration, type ProbeResult, type StorageForm, type StorageState,
} from '@/lib/storage';
import { cn } from '@/lib/utils';

// AdminStorage configures where course media is kept: an S3 compatible
// bucket, which files go there, how browsers get them, and moving files
// between the bucket and the server's disk.
export function AdminStorage() {
  const [st, setSt] = useState<StorageState | null>(null);
  const [f, setF] = useState<StorageForm | null>(null);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState<'' | 'save' | 'test'>('');
  const [probe, setProbe] = useState<ProbeResult | null>(null);

  const show = (s: StorageState) => { setSt(s); setF(formOf(s)); };
  useEffect(() => { api<StorageState>('GET', '/api/admin/storage').then(show).catch((e) => setErr(e.message)); }, []);

  if (err && !st) return <ErrorBox message={err} />;
  if (!st || !f) return null;

  const set = <K extends keyof StorageForm>(k: K, v: StorageForm[K]) => setF((x) => (x ? { ...x, [k]: v } : x));
  const text = (k: 'endpoint' | 'region' | 'bucket' | 'accessKey' | 'secretKey' | 'prefix' | 'cdnBase' | 'presignMinutes') =>
    (e: { target: { value: string } }) => set(k, e.target.value);

  async function save() {
    setBusy('save');
    setErr('');
    try {
      show(await api<StorageState>('PUT', '/api/admin/storage', payload(f!)));
      toast.success('已保存存储设置');
    } catch (ex) {
      setErr(ex instanceof ApiError ? ex.message : '保存失败');
    } finally {
      setBusy('');
    }
  }

  async function test() {
    setBusy('test');
    setErr('');
    setProbe(null);
    try {
      setProbe(await api<ProbeResult>('POST', '/api/admin/storage/test', payload(f!)));
    } catch (ex) {
      setErr(ex instanceof ApiError ? ex.message : '测试失败');
    } finally {
      setBusy('');
    }
  }

  const status = st.status;
  // switched off, only the switch itself and saving stay editable; the rest
  // keeps its values for when it is switched on again
  const off = !f.enabled;
  return (
    <div className="sections storage-settings">
      <Section no="01" title="对象存储" desc="S3 兼容的对象存储, 如 AWS S3、Cloudflare R2、阿里云 OSS、MinIO、RustFS。默认部署自带一个 RustFS。">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
          <span className={cn('st', status.active ? 'ok' : status.connected ? 'warn' : 'off')}>
            {status.active ? '已启用, 媒体文件存入对象存储' : status.connected ? '已连接, 但未启用' : '未连接, 文件都存在服务器本地'}
          </span>
          {status.error ? <span className="text-xs text-ink-3">{status.error}</span> : null}
        </div>
        <Toggle checked={f.enabled} onChange={(v) => set('enabled', v)} label="启用对象存储">关闭后新文件存在本地; 已在桶里的文件仍从桶里读取。下面的设置在打开后才能修改。</Toggle>
        <fieldset className="storage-fields" disabled={off}>
        <div className="grid grid-cols-1 gap-x-4 sm:grid-cols-2">
          <div className="field"><label htmlFor="se">Endpoint</label><Input id="se" className="font-mono" placeholder="https://s3.amazonaws.com" value={f.endpoint} onChange={text('endpoint')} /></div>
          <div className="field"><label htmlFor="sr">Region</label><Input id="sr" className="font-mono" placeholder="us-east-1" value={f.region} onChange={text('region')} /></div>
          <div className="field"><label htmlFor="sb">Bucket</label><Input id="sb" className="font-mono" value={f.bucket} onChange={text('bucket')} /></div>
          <div className="field"><label htmlFor="sp">前缀</label><Input id="sp" className="font-mono" placeholder="留空, 或形如 keine/" value={f.prefix} onChange={text('prefix')} /></div>
          <div className="field"><label htmlFor="sa">Access Key</label><Input id="sa" className="font-mono" autoComplete="off" value={f.accessKey} onChange={text('accessKey')} /></div>
          <div className="field"><label htmlFor="ss">Secret Key</label>
            <Input
              id="ss" type="password" autoComplete="off" disabled={!st.masterKey} value={f.secretKey} onChange={text('secretKey')}
              placeholder={st.secretFromEnv ? '使用环境变量里的密钥' : st.secretSet ? `已保存 ····${st.secretLast4 ?? ''}, 留空则不改` : st.masterKey ? '' : '不能保存密钥'}
            />
            {st.masterKey ? null : <span className="hint warn">服务器没有配置 KEINE_MASTER_KEY, 只能用环境变量 KEINE_S3_SECRET_KEY 里的密钥。</span>}
          </div>
        </div>
        <Toggle checked={f.pathStyle} onChange={(v) => set('pathStyle', v)} label="Path-style 寻址">按 endpoint/bucket 访问; MinIO、RustFS 等自建服务一般需要打开。</Toggle>
        <Toggle checked={f.insecure} onChange={(v) => set('insecure', v)} label="不用 TLS">Endpoint 没写 http:// 或 https:// 时走明文 HTTP。</Toggle>
        <Toggle checked={f.createBucket} onChange={(v) => set('createBucket', v)} label="自动创建 Bucket">Bucket 不存在时创建它。</Toggle>
        </fieldset>
        {probe ? <ProbeSteps res={probe} /> : null}
      </Section>

      <Section no="02" title="上传范围" desc="哪些课程文件放到对象存储。文字稿和课程清单总是留在本地, 读起来快。">
        <fieldset className="storage-fields" disabled={off}>
          <Choices value={f.media} options={MEDIA} onChange={(v) => set('media', v)} name="上传范围" />
        </fieldset>
      </Section>

      <Section no="03" title="访问方式" desc="读者打开视频或图片时怎么拿到文件。权限总由 Keine 先检查, 再给出地址。">
        <fieldset className="storage-fields" disabled={off}>
        <Choices value={f.delivery} options={DELIVERY} onChange={(v) => set('delivery', v)} name="访问方式" />
        {f.delivery === 'presign' ? (
          <div className="field"><label htmlFor="sx">Presign 有效期 (分钟)</label>
            <Input id="sx" inputMode="numeric" value={f.presignMinutes} onChange={text('presignMinutes')} />
            <span className="hint">10 分钟到 7 天。HLS 播放列表 (.m3u8) 和 SVG 仍由服务器转发。</span>
          </div>
        ) : null}
        {f.delivery === 'cdn' ? (
          <div className="field"><label htmlFor="sc">CDN 地址</label>
            <Input id="sc" className="font-mono" placeholder="https://cdn.example.com" value={f.cdnBase} onChange={text('cdnBase')} />
            <span className="hint">文件地址为「CDN 地址/前缀+对象名」。CDN 的源站要指向这个桶, 并能读取它 (公开读或回源鉴权)。</span>
          </div>
        ) : null}
        </fieldset>
      </Section>

      <div className="storage-foot">
        {err ? <ErrorBox message={err} /> : null}
        <div className="form-foot">
          <Button onClick={test} disabled={!!busy || off || !f.endpoint.trim() || !f.bucket.trim()}>{busy === 'test' ? '测试中…' : '测试连接 (读/写)'}</Button>
          <Button variant="default" onClick={save} disabled={!!busy}>{busy === 'save' ? '保存中…' : '保存'}</Button>
        </div>
        <span className="hint">保存前会先连接对象存储, 连不上就不保存。测试连接用表单里还没保存的设置。</span>
      </div>

      <Section no="04" title="迁移" desc="在服务器本地和对象存储之间搬运已有的课程文件。用的是已保存的设置; 迁移期间课程照常可读。">
        <Migrations state={st} onChange={(m) => setSt((x) => (x ? { ...x, migration: m } : x))} />
      </Section>
    </div>
  );
}

function Section({ no, title, desc, children }: { no: string; title: string; desc: string; children: ReactNode }) {
  return (
    <section className="section">
      <div className="section-head"><div className="no">{no}</div><h2>{title}</h2><p>{desc}</p></div>
      <div className="form-rows storage-rows">{children}</div>
    </section>
  );
}

function Toggle({ checked, onChange, label, children }: { checked: boolean; onChange: (v: boolean) => void; label: string; children: ReactNode }) {
  return (
    <label className="storage-toggle">
      <Switch.Root className="switch" checked={checked} onCheckedChange={onChange}><Switch.Thumb className="switch-thumb" /></Switch.Root>
      <span><b>{label}</b><span className="hint">{children}</span></span>
    </label>
  );
}

function Choices<T extends string>({ value, options, onChange, name }: { value: T; options: [T, string, string][]; onChange: (v: T) => void; name: string }) {
  return (
    <div className="storage-choices" role="radiogroup" aria-label={name}>
      {options.map(([v, label, desc]) => (
        <button key={v} type="button" role="radio" aria-checked={value === v} className={cn('storage-choice', value === v && 'on')} onClick={() => onChange(v)}>
          <b>{label}</b><span>{desc}</span>
        </button>
      ))}
    </div>
  );
}

function ProbeSteps({ res }: { res: ProbeResult }) {
  return (
    <div className="storage-probe" role="status">
      <span className={cn('st', res.ok ? 'ok' : 'off')}>{res.ok ? '连接正常, 可以读写' : '测试没有通过'}</span>
      <ol>
        {res.steps.map((s) => (
          <li key={s.name} className={s.ok ? 'ok' : 'bad'}>
            <span>{STEP_NAMES[s.name] ?? s.name}</span><span className="ms">{s.ms} ms</span>
            {s.error ? <span className="err">{s.error}</span> : null}
          </li>
        ))}
      </ol>
    </div>
  );
}

function Migrations({ state, onChange }: { state: StorageState; onChange: (m: Migration | null) => void }) {
  const [asking, setAsking] = useState<Direction | null>(null);
  const m = state.migration;
  const running = m?.state === 'running';

  // poll while one runs
  useEffect(() => {
    if (!running) return;
    const t = setInterval(() => {
      api<StorageState>('GET', '/api/admin/storage').then((s) => onChange(s.migration)).catch(() => {});
    }, 1000);
    return () => clearInterval(t);
  }, [running]);

  async function start(d: Direction) {
    try {
      onChange(await api<Migration>('POST', '/api/admin/storage/migration', { direction: d }));
      setAsking(null);
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '没能开始迁移');
    }
  }

  async function cancel() {
    try { await api('DELETE', '/api/admin/storage/migration'); } catch (ex) { toast.error(ex instanceof ApiError ? ex.message : '没能取消'); }
  }

  const blocked = (d: Direction) => !state.status.connected || (d === 'push' && !state.status.active);
  const asked = DIRECTIONS.find(([d]) => d === asking);
  return (
    <>
      {!state.status.connected ? <span className="st warn">先保存一个能连上的对象存储。</span> : null}
      <div className="storage-migrations">
        {DIRECTIONS.map(([d, label, desc]) => (
          <div key={d} className="storage-migration">
            <div><b>{label}</b><span className="hint">{desc}</span></div>
            <Button size="sm" variant={d === 'push' ? 'default' : 'outline'} disabled={running || blocked(d)} onClick={() => setAsking(d)}>{label}</Button>
          </div>
        ))}
      </div>
      {!state.status.active && state.status.connected ? <span className="hint">推送需要先启用对象存储, 否则移走的文件读不到。</span> : null}
      {m ? (
        <div className="storage-progress" aria-live="polite">
          <div className="bar"><i style={{ width: `${Math.round(progressOf(m) * 100)}%` }} /></div>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className={cn('text-[13px]', m.state === 'failed' && 'st off')}>{migrationLine(m)}</span>
            {running ? <Button size="sm" variant="ghost" onClick={cancel}>取消</Button> : null}
          </div>
          {m.error ? <ErrorBox message={m.error} /> : null}
          {m.errors?.length ? <ErrorBox message="这些文件没有迁移:" problems={m.errors.map((e) => ({ message: e }))} /> : null}
        </div>
      ) : null}
      <Confirm
        open={!!asking} title={asked ? `开始${asked[1]}` : ''} confirm={asked ? `开始${asked[1]}` : ''}
        onConfirm={() => (asking ? start(asking) : undefined)} onClose={() => setAsking(null)}
      >
        {asked?.[2]}。{asking === 'push' ? '本地副本会在上传确认后删除。' : '目标端已有、大小相同的文件会跳过。'}
      </Confirm>
    </>
  );
}
