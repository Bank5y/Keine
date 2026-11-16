import { useCallback, useEffect, useMemo, useRef, useState, type ImgHTMLAttributes } from 'react';
import { ArrowLeft, Archive, MapPin, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { Floating, firstLine, spotOf, supported, useTextMap, type Spot } from '@/components/annotator';
import { Empty } from '@/components/common';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { Skeleton } from '@/components/skeleton';
import { Tabs } from '@/components/tabs';
import { placementOf, useWand, WandBar, WandSuggestion, type Suggestion } from '@/components/wand';
import { api, ApiError } from '@/lib/api';
import { HIGHLIGHT_COLORS, keepAsExcerpt, lookName, placeAnnotation, type Annotation, type Quote, type TextMap } from '@/lib/annotations';
import { lessonURL, type LessonText } from '@/lib/courses';
import { diffText } from '@/lib/diff';
import { Link } from '@/lib/router';
import { cn } from '@/lib/utils';

interface PreviousText { id: number; markdown: string; replacedAt: string }

const failed = (ex: unknown) => toast.error(ex instanceof ApiError ? ex.message : '操作失败');

// useMigration holds the reader's annotations on a lesson and the three ways
// to settle one awaiting migration. place is the one action that puts an
// annotation into the new text, whatever picked the spot: a selection here,
// a suggestion, a drop.
export function useMigration(courseId: number, lessonId: number) {
  const [anns, setAnns] = useState<Annotation[] | null>(null);
  const [err, setErr] = useState('');
  useEffect(() => {
    let alive = true;
    api<{ annotations: Annotation[] }>('GET', `/api/courses/${courseId}/lessons/${lessonId}/annotations`)
      .then((r) => { if (alive) setAnns(r.annotations); })
      .catch((e) => { if (alive) setErr(e.message); });
    return () => { alive = false; };
  }, [courseId, lessonId]);
  const pending = useMemo(() => (anns ?? []).filter((a) => a.state === 'pending'), [anns]);

  const update = (id: number, f: (a: Annotation) => Annotation | null) =>
    setAnns((list) => (list ?? []).flatMap((a) => (a.id === id ? (f(a) ?? []) : [a])));

  const place = useCallback(async (id: number, at: Quote, quiet = false) => {
    try {
      const a = await placeAnnotation(id, at);
      update(id, () => a);
      if (!quiet) toast.success('已迁移到新正文');
      return true;
    } catch (ex) { failed(ex); return false; }
  }, []);
  const keep = useCallback(async (id: number) => {
    try {
      await keepAsExcerpt(id);
      update(id, (a) => ({ ...a, state: 'changed', range: null }));
      toast.success('已保留为原文摘录');
    } catch (ex) { failed(ex); }
  }, []);
  const remove = useCallback(async (id: number) => {
    try {
      await api('DELETE', `/api/annotations/${id}`);
      update(id, () => null);
      toast.success('已删除标注');
    } catch (ex) { failed(ex); }
  }, []);
  return { anns, pending, err, place, keep, remove };
}

// paint sets one named CSS highlight while mounted
function usePaint(name: string, ranges: Range[], priority = 0) {
  useEffect(() => {
    if (!supported()) return undefined;
    const h = new Highlight(...ranges);
    h.priority = priority;
    CSS.highlights.set(name, h);
    return () => { CSS.highlights.delete(name); };
  }, [name, ranges, priority]);
}

const rangesOf = (map: TextMap | null, spans: { start: number; end: number }[]) =>
  map ? spans.flatMap((s) => map.domRanges(s.start, s.end) ?? []) : [];

// an image of a previous text may point at files a later update dropped
function OldImage({ node: _n, ...p }: ImgHTMLAttributes<HTMLImageElement> & { node?: unknown }) {
  const [gone, setGone] = useState(false);
  if (gone) return <span className="mig-img-gone" data-chrome>[图片已不可用{p.alt ? `：${p.alt}` : ''}]</span>;
  return <img {...p} loading="lazy" alt={p.alt || ''} onError={() => setGone(true)} />;
}
const oldComponents = { img: OldImage };

// MigrateView is annotation migration for one lesson, laid out like a split
// diff: the previous text on the left with the reader's annotations awaiting
// migration painted on it, the new text on the right, removed and added text
// marked. Selecting text on the right offers to put the chosen annotation
// there; each one can also be kept as an excerpt or deleted.
export function MigrateView({ courseId, lessonId }: { courseId: number; lessonId: number }) {
  const [lesson, setLesson] = useState<LessonText | null>(null);
  const [previous, setPrevious] = useState<PreviousText[] | null>(null);
  const [loadErr, setLoadErr] = useState('');
  const { anns, pending, err, place, keep, remove } = useMigration(courseId, lessonId);
  const [chosen, setChosen] = useState<number | null>(null);
  const [side, setSide] = useState<'old' | 'new'>('old');
  const [oldRoot, setOldRoot] = useState<HTMLElement | null>(null);
  const [newRoot, setNewRoot] = useState<HTMLElement | null>(null);
  const oldMap = useTextMap(oldRoot);
  const newMap = useTextMap(newRoot);
  const [pick, setPick] = useState<{ spot: Spot; quote: Quote } | null>(null);
  const bar = useRef<HTMLDivElement>(null);
  const wand = useWand(courseId, lessonId);
  const [accepting, setAccepting] = useState(false);

  useEffect(() => {
    let alive = true;
    Promise.all([
      api<LessonText>('GET', `/api/courses/${courseId}/lessons/${lessonId}`),
      api<{ previousTexts: PreviousText[] }>('GET', `/api/courses/${courseId}/lessons/${lessonId}/previous-texts`),
    ]).then(([l, p]) => {
      if (!alive) return;
      setLesson(l);
      setPrevious(p.previousTexts);
      document.title = `迁移标注 · ${l.title} · Keine`;
    }).catch((e) => { if (alive) setLoadErr(e.message); });
    return () => { alive = false; document.title = 'Keine'; };
  }, [courseId, lessonId]);

  // the chosen annotation stays one that awaits migration
  const active = pending.find((a) => a.id === chosen) ?? pending[0] ?? null;
  // the left shows the text the chosen annotation was last shown in
  const oldText = useMemo(() => {
    if (!previous?.length) return null;
    const want = active?.previousTextId ?? pending.find((a) => a.previousTextId != null)?.previousTextId;
    return previous.find((p) => p.id === want) ?? previous[0];
  }, [previous, active, pending]);

  const diff = useMemo(() => (oldMap && newMap ? diffText(oldMap.text, newMap.text) : null), [oldMap, newMap]);
  const removed = useMemo(() => rangesOf(oldMap, diff?.removed ?? []), [oldMap, diff]);
  const added = useMemo(() => rangesOf(newMap, diff?.added ?? []), [newMap, diff]);
  usePaint('mig-del', removed);
  usePaint('mig-add', added);

  // where each annotation awaiting migration sits in the previous text
  const onOld = useMemo(() => {
    const out = new Map<number, Range[]>();
    if (oldMap && oldText) {
      for (const a of pending) {
        if (a.previousTextId != null && a.previousTextId !== oldText.id) continue;
        const at = oldMap.find(a);
        const r = at && oldMap.domRanges(at.start, at.end);
        if (r) out.set(a.id, r);
      }
    }
    return out;
  }, [oldMap, oldText, pending]);
  const looks = useMemo(() => Object.fromEntries(['underline', ...HIGHLIGHT_COLORS].map((look) =>
    [look, pending.filter((a) => lookName(a) === look).flatMap((a) => onOld.get(a.id) ?? [])])), [pending, onOld]);
  usePaint('ann-underline', looks.underline, 1);
  usePaint('ann-yellow', looks.yellow);
  usePaint('ann-green', looks.green);
  usePaint('ann-blue', looks.blue);
  usePaint('ann-pink', looks.pink);
  const activeRanges = useMemo(() => (active ? onOld.get(active.id) ?? [] : []), [active, onOld]);
  usePaint('ann-active', activeRanges, 2);

  // the wand's suggestion for the chosen annotation is marked in the new text
  const suggested = active ? wand.byId.get(active.id) : undefined;
  const suggestedRanges = useMemo(() => rangesOf(newMap, suggested ? [suggested.range] : []), [newMap, suggested]);
  usePaint('mig-suggest', suggestedRanges, 1);
  const showSuggestion = (a: Annotation, s: Suggestion) => {
    setChosen(a.id);
    setSide('new');
    const line = firstLine(rangesOf(newMap, [s.range]));
    if (line) window.scrollTo({ top: line.top + scrollY - innerHeight / 3, behavior: 'smooth' });
  };
  const accept = async (a: Annotation, s: Suggestion) => {
    setAccepting(true);
    await place(a.id, placementOf(s));
    setAccepting(false);
  };
  // accepting every suggestion places each like accepting it alone
  const acceptAll = async () => {
    setAccepting(true);
    let n = 0;
    for (const a of pending) {
      const s = wand.byId.get(a.id);
      if (s && await place(a.id, placementOf(s), true)) n++;
    }
    setAccepting(false);
    if (n) toast.success(`已按建议迁移 ${n} 条标注`);
  };
  const suggestedCount = pending.filter((a) => wand.byId.has(a.id)).length;

  // choosing an annotation brings its place in the previous text into view
  const choose = (a: Annotation) => {
    setChosen(a.id);
    const line = firstLine(onOld.get(a.id) ?? []);
    if (line && oldRoot?.offsetParent) window.scrollTo({ top: line.top + scrollY - innerHeight / 3, behavior: 'smooth' });
  };

  // a selection in the new text offers to put the chosen annotation there
  useEffect(() => {
    if (!newMap || !newRoot) return undefined;
    let t = 0;
    const read = () => {
      const s = getSelection();
      if (!s || s.isCollapsed || !s.rangeCount) { setPick(null); return; }
      const range = s.getRangeAt(0);
      const quote = newRoot.contains(range.commonAncestorContainer) ? newMap.quote(range) : null;
      setPick(quote ? { spot: spotOf(range.getBoundingClientRect()), quote } : null);
    };
    const onChange = () => { clearTimeout(t); t = window.setTimeout(read, 150); };
    document.addEventListener('selectionchange', onChange);
    return () => { clearTimeout(t); document.removeEventListener('selectionchange', onChange); };
  }, [newMap, newRoot]);

  const placeHere = async () => {
    if (!active || !pick) return;
    if (await place(active.id, pick.quote)) {
      getSelection()?.removeAllRanges();
      setPick(null);
    }
  };

  const error = loadErr || err;
  if (error) return <div className="page enter"><Empty glyph="阙" title={error} /></div>;
  const back = <Link className="back" href={lessonURL(courseId, lessonId)}><ArrowLeft />返回本讲</Link>;
  if (!lesson || !previous || !anns) {
    return <div className="page enter mig">{back}<Skeleton className="h-10 w-2/3" /><Skeleton className="mt-6 h-64" /></div>;
  }

  return (
    <div className="page enter mig">
      {back}
      <header className="mig-head">
        <div className="kicker">标注迁移</div>
        <h1>{lesson.title}</h1>
        <p className="sub">
          {pending.length
            ? <>这一讲更新后，你有 <b>{pending.length}</b> 条标注没能自动对回原文。选中一条，在新正文里选中对应的文字，点「放到这里」；也可以把它保留为原文摘录，或者删除。</>
            : '这一讲的标注都已处理完。'}
        </p>
      </header>

      {pending.length ? (
        <>
          <WandBar ran={wand.ran} busy={wand.busy || accepting} count={suggestedCount} onRun={wand.run} onAcceptAll={acceptAll} />
          <ul className="mig-list" aria-label="待迁移的标注">
            {pending.map((a) => (
              <li key={a.id} className={cn('mig-card', a.id === active?.id && 'active')} data-annotation={a.id}>
                <button type="button" className="mig-pick" aria-pressed={a.id === active?.id} onClick={() => choose(a)}>
                  <span className={cn('nq-text', `look-${lookName(a)}`)}>{a.quote}</span>
                  {a.note ? <span className="mig-note">{a.note}</span> : null}
                  {a.previousTextId != null && !onOld.has(a.id) && a.previousTextId === oldText?.id ? <span className="mig-lost">旧版正文里也找不到这段文字</span> : null}
                </button>
                {wand.ran ? (
                  <WandSuggestion suggestion={wand.byId.get(a.id)} busy={accepting}
                    onShow={() => { const s = wand.byId.get(a.id); if (s) showSuggestion(a, s); }}
                    onAccept={() => { const s = wand.byId.get(a.id); if (s) accept(a, s); }} />
                ) : null}
                <div className="mig-actions">
                  <button type="button" className="ann-btn" onClick={() => keep(a.id)}><Archive />保留为原文摘录</button>
                  <button type="button" className="ann-btn danger" onClick={() => remove(a.id)}><Trash2 />删除</button>
                </div>
              </li>
            ))}
          </ul>

          <Tabs className="mig-tabs" tabClassName="mig-tab" value={side} onChange={setSide} items={[['old', '旧版正文'], ['new', '新正文']]} />
          <div className={cn('mig-split', `show-${side}`)}>
            <section className="mig-pane old" aria-label="旧版正文">
              <div className="mig-pane-head">
                <span>旧版正文</span>
                {oldText ? <time dateTime={oldText.replacedAt}>{new Date(oldText.replacedAt).toLocaleDateString('zh-CN')} 更新前</time> : null}
              </div>
              {oldText
                ? <LazyMarkdown key={oldText.id} text={oldText.markdown} base={lesson.base} className="reader-prose mig-prose" components={oldComponents} rootRef={setOldRoot} />
                : <p className="text-ink-3">更新前这一讲没有文字。</p>}
            </section>
            <section className="mig-pane new" aria-label="新正文">
              <div className="mig-pane-head"><span>新正文</span><span className="mig-hint">选中文字，把标注放到这里</span></div>
              {lesson.markdown
                ? <LazyMarkdown text={lesson.markdown} base={lesson.base} className="reader-prose mig-prose" rootRef={setNewRoot} />
                : <p className="text-ink-3">这一讲现在没有文字。</p>}
            </section>
          </div>
        </>
      ) : (
        <Empty glyph="毕" title="没有待迁移的标注">
          <Link className="mig-done" href={lessonURL(courseId, lessonId)}>回到本讲继续阅读</Link>
        </Empty>
      )}

      {pick && active ? (
        <Floating spot={pick.spot} ref={bar} label="迁移标注">
          <span className="ann-quote" title={active.quote}>{active.quote}</span>
          <button type="button" className="ann-btn primary" onClick={placeHere}><MapPin />放到这里</button>
        </Floating>
      ) : null}
    </div>
  );
}
