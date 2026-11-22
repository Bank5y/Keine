import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { ChevronLeft, Download, RotateCcw } from 'lucide-react';
import { documentParts } from '@/components/analysis-popover';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { ApiError } from '@/lib/api';
import {
  blankWriting, generateReport, getReport, listReports, markdownUrl, refusalTitle, shownVersion, stepWriting,
  type Reference, type Report, type ReportPart, type ReportVersion, type Writing,
} from '@/lib/reports';
import { cn } from '@/lib/utils';

type Refused = { code: string; message: string };

const when = (at: string) => {
  const d = new Date(at);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' });
};

// ReportView is the exploration report's drawer (ADR 0012, #160), opened
// over the dialogue from its header: the newest version, or, before the
// first, a new one written at once. A new version streams in its parts;
// the reader may switch versions and download one as Markdown. It is read
// only. Leaving it stops a version being written.
export function ReportView({ exp, onBack }: { exp: number; onBack: () => void }) {
  const [list, setList] = useState<ReportVersion[] | null>(null);
  const [chosen, setChosen] = useState<number | null>(null);
  const [reports, setReports] = useState<Record<number, Report>>({});
  const [writing, setWriting] = useState<Writing | null>(null);
  const [refused, setRefused] = useState<Refused | null>(null);
  const abort = useRef<AbortController | null>(null);
  const scroller = useRef<HTMLDivElement>(null);
  useEffect(() => () => abort.current?.abort(), []);

  const generate = async () => {
    abort.current?.abort();
    const ac = new AbortController();
    abort.current = ac;
    setRefused(null);
    setWriting(blankWriting);
    const end = await generateReport(exp, (e) => setWriting((w) => (w ? stepWriting(w, e) : w)), ac.signal);
    if (ac.signal.aborted) return;
    setWriting(null);
    if (!end.ok) { setRefused({ code: end.code, message: end.message }); return; }
    const r = end.report;
    setReports((m) => ({ ...m, [r.version]: r }));
    setList((l) => [{ id: r.id, version: r.version, title: r.title, createdAt: r.createdAt }, ...(l ?? []).filter((v) => v.version !== r.version)]);
    setChosen(r.version);
  };

  useEffect(() => {
    let gone = false;
    listReports(exp).then((l) => {
      if (gone) return;
      setList(l);
      if (!l.length) void generate();
    }, (e) => { if (!gone) { setList([]); setRefused({ code: 'load', message: e instanceof ApiError ? e.message : '报告没有加载出来，请稍后再试。' }); } });
    return () => { gone = true; };
  }, [exp]); // eslint-disable-line react-hooks/exhaustive-deps

  const shown = list ? shownVersion(list, chosen) : null;
  useEffect(() => {
    if (shown == null || reports[shown]) return undefined;
    let gone = false;
    getReport(exp, shown).then((r) => { if (!gone) setReports((m) => ({ ...m, [r.version]: r })); }, () => {});
    return () => { gone = true; };
  }, [exp, shown]); // eslint-disable-line react-hooks/exhaustive-deps

  // stay at the end as a version streams in
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && writing) el.scrollTop = el.scrollHeight;
  }, [writing]);

  const report = shown != null ? reports[shown] : undefined;
  return (
    <div className="dlg-node dlg-report">
      <div className="dlg-node-bar dlg-report-bar">
        <button type="button" className="dlg-back" onClick={onBack}><ChevronLeft />回到对话</button>
        <span className="grow" />
        {list && list.length && !writing ? (
          <>
            <select className="dlg-report-version" aria-label="报告版本" value={shown ?? ''} onChange={(e) => setChosen(Number(e.target.value))}>
              {list.map((v) => <option key={v.version} value={v.version}>第 {v.version} 版{v.version === list[0].version ? '（最新）' : ''} · {when(v.createdAt)}</option>)}
            </select>
            {shown != null ? <a className="dlg-act" href={markdownUrl(exp, shown)} download><Download />下载 Markdown</a> : null}
            <button type="button" className="dlg-act" onClick={() => void generate()}><RotateCcw />生成新版本</button>
          </>
        ) : null}
      </div>
      <div className="dlg-scroll" ref={scroller}>
        <div className="dlg-col">
          {refused ? (
            <div className="dlg-stop" role="alert">
              <div><b>{refusalTitle(refused.code)}</b><span>{refused.message}</span></div>
              {refused.code !== 'report_too_long' && refused.code !== 'load' ? <button type="button" onClick={() => void generate()}><RotateCcw />重试</button> : null}
            </div>
          ) : null}
          {writing ? (
            <div aria-live="polite" aria-busy="true">
              <p className={cn('analysis-wait', writing.thinking && 'thinking')}><span className="analysis-dot" aria-hidden />{writing.thinking ? '正在思考…' : '正在生成探索报告…'}</p>
              <ReportBody parts={writing.parts} writing />
            </div>
          ) : report ? (
            <ReportBody parts={report} references={report.references} />
          ) : list && list.length ? <p className="analysis-wait">正在加载…</p> : null}
        </div>
      </div>
    </div>
  );
}

// ReportBody is a report's parts in order: the title, the summary, the
// body, the points and (once written) the references.
function ReportBody({ parts: p, references, writing }: { parts: Record<ReportPart, string>; references?: Reference[]; writing?: boolean }) {
  const last = writing ? (['points', 'body', 'summary', 'title'] as const).find((k) => p[k]) : undefined;
  return (
    <article className="dlg-report-doc">
      {p.title ? <h1 className={cn(last === 'title' && 'caret')}>{p.title}</h1> : null}
      {p.summary ? (
        <section><h2>摘要</h2><p className={cn('dlg-report-summary', last === 'summary' && 'caret')}>{p.summary}</p></section>
      ) : null}
      {p.body ? <div className={cn('dlg-reply prose-wrap', last === 'body' && 'caret')}><LazyMarkdown text={p.body} components={documentParts} /></div> : null}
      {p.points ? (
        <section><h2>要点</h2><div className={cn('dlg-reply prose-wrap', last === 'points' && 'caret')}><LazyMarkdown text={p.points} components={documentParts} /></div></section>
      ) : null}
      {references?.length ? (
        <section className="dlg-report-refs">
          <h2>参考</h2>
          <ol>{references.map((r) => <li key={r.url}><a href={r.url} target="_blank" rel="noreferrer">{r.title || r.url}</a></li>)}</ol>
        </section>
      ) : null}
    </article>
  );
}
