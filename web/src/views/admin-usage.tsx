import { useEffect, useState, type ReactNode } from 'react';
import { ArrowLeft } from 'lucide-react';
import { Empty, ErrorBox } from '@/components/common';
import { api } from '@/lib/api';
import { allTokens, credits, quota, rangeOf, tokens, type Account, type DateRange, type Day, type Preset, type ReaderUsage, type Run, type Totals } from '@/lib/credits';
import { Link, match } from '@/lib/router';
import type { User } from '@/lib/session';
import { cn } from '@/lib/utils';

const PRESETS: [Preset, string][] = [['today', '今天'], ['7d', '近 7 天'], ['30d', '近 30 天']];

// AdminUsage is what the assistant cost (#130): the whole site day by day,
// each reader, and one reader's runs, over the days the admin picks. The
// views are tabs so others (charts from a metrics service) can join them.
export function AdminUsage({ path }: { path: string }) {
  const [preset, setPreset] = useState<Preset | 'custom'>('30d');
  const [range, setRange] = useState<DateRange>(() => rangeOf('30d'));
  const reader = match('/admin/usage/users/:id', path)?.id;
  const view = reader ? 'reader' : path.startsWith('/admin/usage/users') ? 'readers' : 'site';

  const pick = (p: Preset) => { setPreset(p); setRange(rangeOf(p)); };
  const custom = (r: DateRange) => { setPreset('custom'); setRange(r); };

  return (
    <>
      <div className="usage-bar">
        {reader ? (
          <Link href="/admin/usage/users" keepScroll className="usage-back"><ArrowLeft />用量 · 用户</Link>
        ) : (
          <nav className="seg" aria-label="用量">
            <Link href="/admin/usage" keepScroll className={cn(view === 'site' && 'on')} aria-current={view === 'site' ? 'page' : undefined}>全站</Link>
            <Link href="/admin/usage/users" keepScroll className={cn(view === 'readers' && 'on')} aria-current={view === 'readers' ? 'page' : undefined}>用户</Link>
          </nav>
        )}
        <div className="usage-range">
          <div className="seg" role="group" aria-label="时间范围">
            {PRESETS.map(([p, label]) => (
              <button key={p} type="button" className={cn(preset === p && 'on')} aria-pressed={preset === p} onClick={() => pick(p)}>{label}</button>
            ))}
          </div>
          <label className="date-range">
            <input type="date" aria-label="开始日期" value={range.from} max={range.to} onChange={(e) => e.target.value && custom({ ...range, from: e.target.value })} />
            <span>–</span>
            <input type="date" aria-label="结束日期" value={range.to} min={range.from} onChange={(e) => e.target.value && custom({ ...range, to: e.target.value })} />
          </label>
        </div>
      </div>
      {view === 'site' ? <SiteUsage range={range} /> : view === 'readers' ? <ReadersUsage range={range} /> : <ReaderDetail id={reader!} range={range} />}
    </>
  );
}

// useReport loads a report for the range, again whenever it changes.
function useReport<T>(url: string, range: DateRange): [T | null, string] {
  const [data, setData] = useState<T | null>(null);
  const [err, setErr] = useState('');
  useEffect(() => {
    let live = true;
    setErr('');
    api<T>('GET', `${url}?from=${range.from}&to=${range.to}`).then((d) => { if (live) setData(d); }).catch((e) => { if (live) setErr(e.message); });
    return () => { live = false; };
  }, [url, range.from, range.to]);
  return [data, err];
}

function Tiles({ total }: { total: Totals }) {
  return (
    <div className="usage-tiles">
      <Tile label="运行次数" value={String(total.runs)} note={total.estimated ? `其中估算 ${total.estimated} 次` : undefined} />
      <Tile label="token 合计" value={tokens(allTokens(total))} note="输入 · 命中 · 写入 · 输出" />
      <Tile label="搜索次数" value={String(total.searches)} />
      <Tile label="积分" value={credits(total.credits)} note="按运行当时的费率" />
    </div>
  );
}

function Tile({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="tile">
      <div className="tile-label">{label}</div>
      <div className="tile-value">{value}</div>
      {note ? <div className="tile-note">{note}</div> : null}
    </div>
  );
}

function DayTable({ days }: { days: Day[] }) {
  if (!days.length) return <Empty title="这段时间没有运行" />;
  const today = rangeOf('today').to;
  return (
    <Table head={['日期', '运行', '输入', '缓存命中', '缓存写入', '输出', '搜索', '积分']}>
      {days.map((d) => (
        <tr key={d.date}>
          <td className="date">{d.date.slice(5).replace('-', '/')}{d.date === today ? <small>今天</small> : null}</td>
          <td>{d.runs}</td><td>{tokens(d.input)}</td><td>{tokens(d.cacheRead)}</td><td>{tokens(d.cacheWrite)}</td>
          <td>{tokens(d.output)}</td><td>{d.searches}</td><td>{credits(d.credits)}</td>
        </tr>
      ))}
    </Table>
  );
}

function Table({ head, children }: { head: string[]; children: ReactNode }) {
  return (
    <div className="usage-table-wrap">
      <table className="usage-table">
        <thead><tr>{head.map((h) => <th key={h} scope="col">{h}</th>)}</tr></thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  );
}

function SiteUsage({ range }: { range: DateRange }) {
  const [data, err] = useReport<{ total: Totals; days: Day[] }>('/api/admin/usage', range);
  if (err) return <ErrorBox message={err} />;
  if (!data) return <p className="usage-loading">正在加载…</p>;
  return (
    <>
      <Tiles total={data.total} />
      <h3 className="usage-h">按天<small>服务器本地时间 0 点分日</small></h3>
      <DayTable days={data.days} />
    </>
  );
}

function ReadersUsage({ range }: { range: DateRange }) {
  const [data, err] = useReport<{ users: ReaderUsage[] }>('/api/admin/usage/users', range);
  if (err) return <ErrorBox message={err} />;
  if (!data) return <p className="usage-loading">正在加载…</p>;
  if (!data.users.length) return <Empty title="这段时间没有用户用过 AI" />;
  return (
    <>
      <Table head={['用户', '套餐', '运行', 'token', '搜索', '积分', '']}>
        {data.users.map((u) => (
          <tr key={u.id}>
            <td className="who"><Link href={`/admin/usage/users/${u.id}`} keepScroll>{u.username}</Link></td>
            <td className="plan">{u.plan}</td>
            <td>{u.runs}</td><td>{tokens(allTokens(u))}</td><td>{u.searches}</td><td>{credits(u.credits)}</td>
            <td className="more"><Link href={`/admin/usage/users/${u.id}`} keepScroll>详情 →</Link></td>
          </tr>
        ))}
      </Table>
      <p className="usage-note">自带模型的运行不计在内。</p>
    </>
  );
}

const KINDS: Record<string, string> = { analysis: '分析', followup: '后续轮', sync: '同步' };
const n = (v: number | null) => (v === null ? '—' : tokens(v));
const at = (s: string) => new Date(s).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });

function ReaderDetail({ id, range }: { id: string; range: DateRange }) {
  const [data, err] = useReport<{ user: User; credits: Account; total: Totals; days: Day[]; runs: Run[] }>(`/api/admin/usage/users/${id}`, range);
  if (err) return <ErrorBox message={err} />;
  if (!data) return <p className="usage-loading">正在加载…</p>;
  const c = data.credits;
  return (
    <>
      <div className="usage-who">
        <h2>{data.user.username}</h2>
        <span>{c.plan} · 今日 {credits(c.used)} / {quota(c.daily)}</span>
      </div>
      <Tiles total={data.total} />
      <h3 className="usage-h">按天</h3>
      <DayTable days={data.days} />
      <h3 className="usage-h">最近的运行<small>最多 100 次; ≈ 是本地估算</small></h3>
      {data.runs.length ? (
        <Table head={['时间', '种类', '模型', '输入', '缓存命中', '缓存写入', '输出', '搜索', '积分']}>
          {data.runs.map((r) => (
            <tr key={r.id}>
              <td className="date">{at(r.at)}</td><td className="plan">{KINDS[r.kind] ?? r.kind}</td><td className="plan">{r.model}</td>
              <td>{n(r.input)}</td><td>{n(r.cacheRead)}</td><td>{n(r.cacheWrite)}</td><td>{n(r.output)}</td><td>{r.searches}</td>
              <td>{r.estimated ? <span title="本地估算">≈ </span> : null}{credits(r.credits)}</td>
            </tr>
          ))}
        </Table>
      ) : <Empty title="这段时间没有运行" />}
    </>
  );
}
