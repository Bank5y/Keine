import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import { NotebookPen, Pencil, Sparkles, Trash2, Underline } from 'lucide-react';
import { toast } from 'sonner';
import { AnalysisPopover, type Anchor } from '@/components/analysis-popover';
import { api, ApiError } from '@/lib/api';
import { ANNOTATION_HASH, COLOR_LABEL, HIGHLIGHT_COLORS, TextMap, lookName, type Annotation, type Look, type Quote } from '@/lib/annotations';
import { ExplorationCard } from '@/components/exploration-card';
import { listExplorations, overlapping, OVERLAP_TEXT, type Exploration } from '@/lib/explorations';
import { useKeyboardInset } from '@/lib/use-keyboard-inset';
import { claimTap } from '@/lib/use-lesson-bar';
import { unavailableText } from '@/lib/ai';
import { useUser } from '@/lib/session';
import { cn } from '@/lib/utils';

// one CSS highlight per look; underlines paint above highlights so an
// underline inside a highlight stays visible, and the annotation in focus
// (its note card picked, or its note being written) paints above both
const LOOKS = ['underline', ...HIGHLIGHT_COLORS] as const;
const highlightName = (look: string) => `ann-${look}`;
const ACTIVE = 'ann-active';
// the annotation the notes page jumped to, blinking
const FLASH = 'ann-flash';
export const supported = () => typeof CSS !== 'undefined' && 'highlights' in CSS;

// Wide screens keep notes in a rail beside the text (see .docs-notes in
// styles.css, which shows the rail at the same width); narrower ones show a
// note in the bar that opens on its annotation.
export const NOTE_RAIL_MEDIA = '(min-width: 1440px)';
// the gap between stacked cards in the rail
const CARD_GAP = 8;
// how far a pinned passage's card reaches past its text, each side
const PASSAGE_PAD_X = 4;
const PASSAGE_PAD_Y = 2;

const failed = (ex: unknown) => toast.error(ex instanceof ApiError ? ex.message : '操作失败');

function useMedia(query: string) {
  const [on, setOn] = useState(() => matchMedia(query).matches);
  useEffect(() => {
    const m = matchMedia(query);
    const onChange = () => setOn(m.matches);
    onChange();
    m.addEventListener('change', onChange);
    return () => m.removeEventListener('change', onChange);
  }, [query]);
  return on;
}

// useTextMap maps the rendered text under root, afresh whenever React
// re-renders nodes inside it
export function useTextMap(root: HTMLElement | null) {
  const [version, setVersion] = useState(0);
  useEffect(() => {
    if (!root) return undefined;
    const mo = new MutationObserver(() => setVersion((v) => v + 1));
    mo.observe(root, { childList: true, subtree: true, characterData: true });
    return () => mo.disconnect();
  }, [root]);
  return useMemo(() => (root ? new TextMap(root) : null), [root, version]); // eslint-disable-line react-hooks/exhaustive-deps
}

// A tap on a bar may clear the selection on a touch screen before the tap
// lands, and a bar that follows the selection would go before its button
// is pressed. barTaps listens for touches on the bar: recent() tells whether
// one began within TAP_GRACE ms, so the bar stays for it; stop() ends it.
const TAP_GRACE = 600;
export function barTaps(bar: RefObject<HTMLElement | null>) {
  let at = -Infinity;
  const onDown = (e: PointerEvent) => { if (e.pointerType !== 'mouse' && bar.current?.contains(e.target as Node)) at = performance.now(); };
  document.addEventListener('pointerdown', onDown, true);
  return { recent: () => performance.now() - at < TAP_GRACE, stop: () => document.removeEventListener('pointerdown', onDown, true) };
}

// a spot on the page, in document coordinates, that a floating bar points at
export interface Spot { x: number; top: number; bottom: number }
export const spotOf = (r: DOMRect): Spot => ({ x: r.left + r.width / 2 + scrollX, top: r.top + scrollY, bottom: r.bottom + scrollY });

// the first line box an annotation's text sits on
export const firstLine = (ranges: Range[]) => ranges.flatMap((r) => Array.from(r.getClientRects())).find((c) => c.width > 0 && c.height > 0) ?? null;

// a note being written: on a selection not yet annotated, or on an annotation
type Editing =
  | { spot: Spot; quote: Quote; ranges: Range[]; look: Look; draft: string }
  | { spot: Spot; id: number; draft: string };

// Annotator lets the reader underline and highlight the text of a lesson and
// note on what he marks: selecting text offers a toolbar, clicking an
// annotation offers to restyle or delete it (each of several, where they
// overlap) and to write its note. On wide screens the notes sit as cards in
// the rail, each level with the line its annotation starts on; elsewhere the
// bar shows them. The annotations are painted with the CSS Custom Highlight
// API, so the rendered Markdown is never touched.
// onPending hears how many of the reader's annotations on the lesson await
// migration; only attached annotations show in the text.
export function Annotator({ courseId, lessonId, root, rail, onPending }: {
  courseId: number; lessonId: number; root: HTMLElement | null; rail: HTMLElement | null; onPending?: (n: number) => void;
}) {
  const [anns, setAnns] = useState<Annotation[]>([]);
  const map = useTextMap(root);
  const placed = useMemo(() => {
    const out = new Map<number, Range[]>();
    if (map) for (const a of anns) { const r = map.place(a); if (r) out.set(a.id, r); }
    return out;
  }, [map, anns]);
  // the reader's explorations, on wide screens only (narrow ones show
  // pinned passages as plain text and ask for none)
  const [exps, setExps] = useState<Exploration[]>([]);
  const placedExps = useMemo(() => {
    const out = new Map<number, Range[]>();
    if (map) for (const e of exps) { const r = map.place(e); if (r) out.set(e.id, r); }
    return out;
  }, [map, exps]);
  // the exploration whose card is picked; reveal: bring its card into view
  const [activeExp, setActiveExp] = useState<number | null>(null);
  const [reveal, setReveal] = useState(0);
  // the exploration whose card is open in the rail (#180); busy: a dialog
  // of that card's is up (its dialogue, the delete confirmation), which
  // takes presses and Esc for itself
  const [openExp, setOpenExp] = useState<number | null>(null);
  const cardBusy = useRef(false);
  const [pick, setPick] = useState<{ spot: Spot; quote: Quote; range: Range; overlaps: boolean } | null>(null);
  const [open, setOpen] = useState<{ spot: Spot; ids: number[] } | null>(null);
  const [editing, setEditing] = useState<Editing | null>(null);
  // the annotation whose note card is picked, or whose text was clicked
  const [active, setActive] = useState<number | null>(null);
  const bar = useRef<HTMLDivElement>(null);
  const wide = useMedia(NOTE_RAIL_MEDIA);
  const railed = wide && !!rail;
  const expsRef = useRef(exps);
  expsRef.current = exps;
  // wide screens offer 分析, greyed out with the reason until the
  // assistant is set up ('' once it is)
  const user = useUser();
  const unavailable = !user || user.aiAvailable ? '' : unavailableText(user.aiProblem, user.role === 'admin');
  const [analysis, setAnalysis] = useState<{ quote: Quote; ranges: Range[]; at: Anchor } | null>(null);
  const analyzing = useRef(false);
  analyzing.current = !!analysis;

  useEffect(() => {
    let alive = true;
    api<{ annotations: Annotation[] }>('GET', `/api/courses/${courseId}/lessons/${lessonId}/annotations`)
      .then((r) => {
        if (!alive) return;
        setAnns(r.annotations.filter((a) => a.state === 'attached'));
        onPending?.(r.annotations.filter((a) => a.state === 'pending').length);
      }).catch(() => {});
    return () => { alive = false; };
  }, [courseId, lessonId]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!wide) return undefined;
    let alive = true;
    listExplorations(courseId, lessonId).then((list) => { if (alive) setExps(list); }).catch(() => {});
    return () => { alive = false; };
  }, [courseId, lessonId, wide]);

  // a pinned passage clicked in the text brings its card into view
  useEffect(() => {
    if (!reveal || activeExp == null) return;
    document.querySelector(`.exp-card[data-exploration="${activeExp}"]`)?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  }, [reveal]); // eslint-disable-line react-hooks/exhaustive-deps

  // paint
  useEffect(() => {
    if (!supported()) return undefined;
    for (const look of LOOKS) {
      const h = new Highlight(...anns.filter((a) => lookName(a) === look).flatMap((a) => placed.get(a.id) ?? []));
      h.priority = look === 'underline' ? 1 : 0;
      CSS.highlights.set(highlightName(look), h);
    }
    return () => { for (const look of LOOKS) CSS.highlights.delete(highlightName(look)); };
  }, [anns, placed]);

  // paint the annotation in focus
  const focused = analysis ? analysis.ranges : editing ? ('id' in editing ? placed.get(editing.id) : editing.ranges) : (active != null ? placed.get(active) : undefined);
  useEffect(() => {
    if (!supported()) return undefined;
    const h = new Highlight(...(focused ?? []));
    h.priority = 2;
    CSS.highlights.set(ACTIVE, h);
    return () => { CSS.highlights.delete(ACTIVE); };
  }, [focused]);

  // opened at "#annotation-<id>" (from the notes page): bring that annotation
  // into view and flash it, once it is on the page
  const jumped = useRef(false);
  const [flash, setFlash] = useState<Range[] | null>(null);
  useEffect(() => {
    if (jumped.current || !location.hash.startsWith(ANNOTATION_HASH)) return;
    const id = Number(location.hash.slice(ANNOTATION_HASH.length));
    if (!placed.has(id)) return;
    jumped.current = true;
    const ranges = placed.get(id)!;
    // images above it may still be growing the text: aim again once they settled
    const aim = () => { const line = firstLine(ranges); if (line) window.scrollTo({ top: line.top + scrollY - innerHeight / 3, behavior: 'instant' }); };
    aim();
    window.setTimeout(aim, 350);
    // on, off, on, off: a short blink, left to run out should the lesson close
    [0, 350, 550, 1100].forEach((at, i) => window.setTimeout(() => setFlash(i % 2 ? null : ranges), at));
  }, [placed]);
  useEffect(() => {
    if (!supported() || !flash) return undefined;
    const h = new Highlight(...flash);
    h.priority = 3;
    CSS.highlights.set(FLASH, h);
    return () => { CSS.highlights.delete(FLASH); };
  }, [flash]);

  // a finished selection inside the text offers the toolbar
  useEffect(() => {
    if (!map || !root) return undefined;
    let down = false;
    let t = 0;
    const taps = barTaps(bar);
    const read = () => {
      // while an analysis is open, a new selection only asks to close it
      if (analyzing.current) return;
      const s = getSelection();
      if (!s || s.isCollapsed || !s.rangeCount) { setPick(null); return; }
      const range = s.getRangeAt(0);
      const quote = root.contains(range.commonAncestorContainer) ? map.quote(range) : null;
      if (!quote) { setPick(null); return; }
      setOpen(null);
      setEditing(null);
      setPick({ spot: spotOf(range.getBoundingClientRect()), quote, range: range.cloneRange(), overlaps: !!overlapping(map.rangeOf(range), expsRef.current) });
    };
    const onChange = () => { clearTimeout(t); if (!down && !taps.recent()) t = window.setTimeout(read, 120); };
    const onDown = (e: PointerEvent) => { if (!bar.current?.contains(e.target as Node)) down = true; };
    const onUp = () => { if (down) { down = false; clearTimeout(t); t = window.setTimeout(read, 0); } };
    document.addEventListener('selectionchange', onChange);
    document.addEventListener('pointerdown', onDown);
    document.addEventListener('pointerup', onUp);
    // a phone takes a long press over for its own selection and cancels the
    // pointer: no pointerup follows, so the cancel ends the press instead
    document.addEventListener('pointercancel', onUp);
    return () => {
      clearTimeout(t);
      taps.stop();
      document.removeEventListener('selectionchange', onChange);
      document.removeEventListener('pointerdown', onDown);
      document.removeEventListener('pointerup', onUp);
      document.removeEventListener('pointercancel', onUp);
    };
  }, [map, root]);

  // a click on annotated text opens what lies under it, and picks its card
  useEffect(() => {
    if (!root) return undefined;
    const onClick = (e: MouseEvent) => {
      const s = getSelection();
      if (s && !s.isCollapsed) return;
      if ((e.target as HTMLElement).closest('a, img')) return;
      const hits: number[] = [];
      let at: DOMRect | null = null;
      for (const a of anns) {
        const rect = (placed.get(a.id) ?? []).flatMap((r) => Array.from(r.getClientRects())).find((c) => e.clientX >= c.left && e.clientX <= c.right && e.clientY >= c.top && e.clientY <= c.bottom);
        if (rect) { hits.push(a.id); at ??= rect; }
      }
      // a pinned passage picks its card, and brings it into view
      const within = (ranges: Range[]) => ranges.flatMap((r) => Array.from(r.getClientRects())).some((c) => e.clientX >= c.left - PASSAGE_PAD_X && e.clientX <= c.right + PASSAGE_PAD_X && e.clientY >= c.top - PASSAGE_PAD_Y && e.clientY <= c.bottom + PASSAGE_PAD_Y);
      const exp = [...placedExps].find(([, ranges]) => within(ranges))?.[0] ?? null;
      if (hits.length || exp != null) claimTap(e);
      setEditing(null);
      setOpen(hits.length && at ? { spot: { x: e.clientX + scrollX, top: at.top + scrollY, bottom: at.bottom + scrollY }, ids: hits } : null);
      setActive(exp != null ? null : hits.find((id) => anns.find((a) => a.id === id)?.note) ?? null);
      setActiveExp(exp);
      setOpenExp(exp);
      if (exp != null) setReveal((n) => n + 1);
    };
    root.addEventListener('click', onClick);
    return () => root.removeEventListener('click', onClick);
  }, [root, anns, placed, placedExps]);

  // an open bar closes on a click elsewhere or on Escape; so does the focus
  // on a card
  useEffect(() => {
    if (!open && !editing && active == null && activeExp == null) return undefined;
    const inside = (t: Node) => bar.current?.contains(t) || root?.contains(t) || (t instanceof Element && t.closest('.note-card, .exp-card, .exp-card-open'));
    const onDown = (e: PointerEvent) => {
      if (cardBusy.current || inside(e.target as Node)) return;
      setOpen(null); setEditing(null); setActive(null); setActiveExp(null); setOpenExp(null);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || cardBusy.current) return;
      setOpen(null); setEditing(null); setActive(null); setActiveExp(null); setOpenExp(null);
    };
    document.addEventListener('pointerdown', onDown);
    document.addEventListener('keydown', onKey);
    return () => { document.removeEventListener('pointerdown', onDown); document.removeEventListener('keydown', onKey); };
  }, [open, editing, active, activeExp, root]);

  const create = async (look: Look, note?: string) => {
    const from = pick ?? (editing && 'quote' in editing ? editing : null);
    if (!from) return;
    try {
      const a = await api<Annotation>('POST', `/api/courses/${courseId}/lessons/${lessonId}/annotations`, { ...look, ...from.quote, ...(note ? { note } : {}) });
      setAnns((list) => [...list, a]);
      getSelection()?.removeAllRanges();
      setPick(null);
      setEditing(null);
      if (a.note) setActive(a.id);
    } catch (ex) { failed(ex); }
  };

  const restyle = async (id: number, look: Look) => {
    try {
      await api('PATCH', `/api/annotations/${id}`, look);
      setAnns((list) => list.map((a) => (a.id === id ? { ...a, style: look.style, color: look.style === 'highlight' ? look.color : null } : a)));
    } catch (ex) { failed(ex); }
  };

  const remove = async (id: number) => {
    try {
      await api('DELETE', `/api/annotations/${id}`);
      setAnns((list) => list.filter((a) => a.id !== id));
      setOpen((o) => (o && o.ids.length > 1 ? { ...o, ids: o.ids.filter((x) => x !== id) } : null));
      setActive((x) => (x === id ? null : x));
    } catch (ex) { failed(ex); }
  };

  // writeNote saves a note on an annotation; a blank one takes it off
  const writeNote = async (id: number, note: string) => {
    try {
      const blank = !note.trim();
      if (blank) await api('DELETE', `/api/annotations/${id}/note`);
      else await api('PUT', `/api/annotations/${id}/note`, { note });
      setAnns((list) => list.map((a) => (a.id === id ? { ...a, note: blank ? null : note } : a)));
      setEditing(null);
      setActive(blank ? null : id);
    } catch (ex) { failed(ex); }
  };

  // analyze opens the analysis popover on a quote, under its text
  const analyze = (quote: Quote, ranges: Range[]) => {
    const rects = ranges.flatMap((r) => Array.from(r.getClientRects())).filter((c) => c.width > 0);
    if (!rects.length) return;
    const left = Math.min(...rects.map((c) => c.left));
    const bottom = Math.max(...rects.map((c) => c.bottom)) + scrollY;
    getSelection()?.removeAllRanges();
    setPick(null);
    setOpen(null);
    setEditing(null);
    setAnalysis({ quote, ranges, at: { left, bottom } });
  };

  // startNote opens the note editor on an annotation, at its text
  const startNote = (a: Annotation) => {
    const line = firstLine(placed.get(a.id) ?? []);
    const spot = open?.spot ?? (line ? spotOf(line) : null);
    if (!spot) return;
    setOpen(null);
    setEditing({ spot, id: a.id, draft: a.note ?? '' });
  };

  const noteActive = editing && 'id' in editing ? editing.id : active;
  let floating: ReactNode = null;
  if (editing) {
    const isNew = 'quote' in editing;
    floating = (
      <Floating spot={editing.spot} ref={bar} label={isNew ? '写笔记' : '编辑笔记'} stacked>
        {isNew ? <div className="ann-row"><Looks current={editing.look.style === 'underline' ? 'underline' : editing.look.color} onPick={(look) => setEditing({ ...editing, look })} /></div> : null}
        <NoteEditor draft={editing.draft} onDraft={(draft) => setEditing({ ...editing, draft })} canClear={!isNew}
          onSave={() => { if (isNew) void create(editing.look, editing.draft.trim() ? editing.draft : undefined); else void writeNote(editing.id, editing.draft); }}
          onCancel={() => setEditing(null)} />
      </Floating>
    );
  } else if (pick) {
    floating = (
      <Floating spot={pick.spot} ref={bar} label="标注">
        <Looks onPick={(look) => create(look)} />
        <span className="ann-sep" />
        <span className="ann-acts">
          <button type="button" className="ann-btn" onClick={() => { setEditing({ spot: pick.spot, quote: pick.quote, ranges: [pick.range], look: { style: 'highlight', color: 'yellow' }, draft: '' }); setPick(null); }}><NotebookPen />写笔记</button>
          {wide ? <AnalyzeButton unavailable={unavailable || (pick.overlaps ? OVERLAP_TEXT : '')} onAnalyze={() => analyze(pick.quote, [pick.range])} /> : null}
        </span>
      </Floating>
    );
  } else if (open) {
    const opened = open.ids.map((id) => anns.find((a) => a.id === id)).filter((a): a is Annotation => !!a);
    // the note shows here unless its card is in the rail
    const shown = (a: Annotation) => !!a.note && !(railed && placed.has(a.id));
    floating = (
      <Floating spot={open.spot} ref={bar} label="编辑标注" stacked={opened.length > 1 || opened.some(shown)}>
        {opened.map((a) => (
          <div key={a.id} className="ann-item" data-annotation={a.id}>
            <div className="ann-row">
              {opened.length > 1 ? <span className="ann-quote" title={a.quote}>{a.quote}</span> : null}
              <Looks current={lookName(a)} onPick={(look) => restyle(a.id, look)} />
              <span className="ann-sep" />
              <span className="ann-acts">
                {a.note ? null : <button type="button" className="ann-btn" onClick={() => startNote(a)}><NotebookPen />写笔记</button>}
                {wide ? <AnalyzeButton unavailable={unavailable || (overlapping(a.range, exps) ? OVERLAP_TEXT : '')} onAnalyze={() => analyze({ quote: a.quote, prefix: a.prefix, suffix: a.suffix }, placed.get(a.id) ?? [])} /> : null}
                <button type="button" className="ann-btn danger" onClick={() => remove(a.id)}><Trash2 />删除</button>
              </span>
            </div>
            {shown(a) ? (
              <div className="ann-note">
                <p className="note-text">{a.note}</p>
                <div className="note-actions">
                  <button type="button" className="ann-btn" onClick={() => startNote(a)}><Pencil />编辑笔记</button>
                  <button type="button" className="ann-btn danger" onClick={() => writeNote(a.id, '')}><Trash2 />删除笔记</button>
                </div>
              </div>
            ) : null}
          </div>
        ))}
      </Floating>
    );
  }

  return (
    <>
      {railed && rail ? (
        <Rail rail={rail} root={root} items={[
          ...anns.filter((a) => a.note && placed.has(a.id)).map((a) => ({
            key: `note-${a.id}`, ranges: placed.get(a.id)!, start: a.range?.start ?? 0, active: a.id === noteActive,
            card: <NoteCard a={a} active={a.id === noteActive} onPick={() => { setOpen(null); setEditing(null); setActiveExp(null); setOpenExp(null); setActive(a.id); }}
              onEdit={() => startNote(a)} onDelete={() => writeNote(a.id, '')} />,
          })),
          ...exps.filter((e) => placedExps.has(e.id)).map((e) => ({
            key: `exp-${e.id}`, ranges: placedExps.get(e.id)!, start: e.range?.start ?? 0, active: e.id === activeExp,
            card: <ExplorationCard e={e} active={e.id === activeExp} open={e.id === openExp} anchor={() => firstLine(placedExps.get(e.id) ?? [])?.top}
              onPick={() => { setOpen(null); setEditing(null); setActive(null); setActiveExp(e.id); setOpenExp(e.id); }}
              onFold={() => setOpenExp(null)} busy={(on) => { cardBusy.current = on; }}
              onDeleted={(id) => { setOpenExp(null); setActiveExp(null); setExps((list) => list.filter((x) => x.id !== id)); }}
              onNodes={(id, nodes) => setExps((list) => list.map((x) => (x.id === id ? { ...x, nodes } : x)))} />,
          })),
        ]} />
      ) : null}
      {wide && root ? <PinnedPassages root={root} placed={placedExps} active={activeExp} /> : null}
      {floating}
      {analysis ? (
        <AnalysisPopover courseId={courseId} lessonId={lessonId} quote={analysis.quote} at={analysis.at} onClose={() => setAnalysis(null)}
          onPinned={(e) => { setExps((list) => [...list, e]); setAnalysis(null); setActive(null); setActiveExp(e.id); }} />
      ) : null}
    </>
  );
}

// RailItem is a card in the rail: a note or an exploration, level with the
// first line of its text.
interface RailItem { key: string; ranges: Range[]; start: number; active: boolean; card: ReactNode }

// Rail lays the cards out in the rail: each level with the first line of
// its text, in the order of the text, pushed down below the one before
// where they would overlap. Notes and explorations share it.
function Rail({ rail, root, items }: { rail: HTMLElement; root: HTMLElement | null; items: RailItem[] }) {
  const slots = useRef(new Map<string, HTMLElement>());
  const [tops, setTops] = useState<Map<string, number>>(new Map());
  // the text and the rail reflow as the window resizes and images load
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const ro = new ResizeObserver(() => setTick((t) => t + 1));
    ro.observe(rail);
    if (root) ro.observe(root);
    return () => ro.disconnect();
  }, [rail, root]);

  useLayoutEffect(() => {
    const base = rail.getBoundingClientRect().top;
    const want = items.map((it) => ({ it, top: (firstLine(it.ranges)?.top ?? base) - base }))
      .sort((x, y) => x.top - y.top || x.it.start - y.it.start);
    const next = new Map<string, number>();
    let end = -Infinity;
    for (const { it, top } of want) {
      const at = Math.max(top, end + CARD_GAP);
      next.set(it.key, at);
      end = at + (slots.current.get(it.key)?.offsetHeight ?? 0);
    }
    setTops((prev) => (prev.size === next.size && [...next].every(([k, t]) => Math.abs((prev.get(k) ?? NaN) - t) < 0.5) ? prev : next));
  }, [rail, items, tick]); // items are new each render: a card grows as it opens

  return createPortal(
    items.map((it) => (
      <div key={it.key} ref={(el) => { if (el) slots.current.set(it.key, el); else slots.current.delete(it.key); }}
        className={cn('rail-slot', it.active && 'active')}
        style={{ top: tops.get(it.key) ?? 0, visibility: tops.has(it.key) ? undefined : 'hidden' }}>
        {it.card}
      </div>
    )),
    rail,
  );
}

function NoteCard({ a, active, onPick, onEdit, onDelete }: { a: Annotation; active: boolean; onPick: () => void; onEdit: () => void; onDelete: () => void }) {
  return (
    <div className={cn('note-card', active && 'active')} data-annotation={a.id} role="button" tabIndex={0}
      aria-pressed={active} aria-label={`笔记：${a.quote}`}
      onClick={onPick} onKeyDown={(e) => { if (e.key === 'Enter' && e.target === e.currentTarget) onPick(); }}>
      <div className={cn('note-quote', `look-${lookName(a)}`)}>{a.quote}</div>
      {/* a card that folds back starts again from its first line */}
      <p className="note-text" ref={(el) => { if (el && !active) el.scrollTop = 0; }}>{a.note}</p>
      <div className="note-actions">
        <button type="button" className="ann-btn" aria-label="编辑笔记" title="编辑笔记" onClick={(e) => { e.stopPropagation(); onEdit(); }}><Pencil /></button>
        <button type="button" className="ann-btn danger" aria-label="删除笔记" title="删除笔记" onClick={(e) => { e.stopPropagation(); onDelete(); }}><Trash2 /></button>
      </div>
    </div>
  );
}

// PinnedPassages draws the text of each exploration as a card raised a
// little off the page, one per line so a passage over several lines has
// full rounded ends on each. The cards sit under the text, so a highlight
// over the same words keeps its colour inside them.
function PinnedPassages({ root, placed, active }: { root: HTMLElement; placed: Map<number, Range[]>; active: number | null }) {
  const host = root.parentElement;
  const [boxes, setBoxes] = useState<{ key: string; id: number; left: number; top: number; width: number; height: number }[]>([]);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const ro = new ResizeObserver(() => setTick((t) => t + 1));
    ro.observe(root);
    return () => ro.disconnect();
  }, [root]);
  useLayoutEffect(() => {
    if (!host) return;
    const base = host.getBoundingClientRect();
    const out: typeof boxes = [];
    for (const [id, ranges] of placed) {
      lineBoxes(ranges).forEach((r, i) => out.push({
        key: `${id}-${i}`, id,
        left: r.left - base.left - PASSAGE_PAD_X, top: r.top - base.top - PASSAGE_PAD_Y,
        width: r.width + 2 * PASSAGE_PAD_X, height: r.height + 2 * PASSAGE_PAD_Y,
      }));
    }
    setBoxes(out);
  }, [host, placed, tick]);
  if (!host) return null;
  return createPortal(
    <div className="pinned-layer" aria-hidden>
      {boxes.map((b) => <span key={b.key} className={cn('pinned-passage', b.id === active && 'active')} style={{ left: b.left, top: b.top, width: b.width, height: b.height }} />)}
    </div>,
    host,
  );
}

// lineBoxes joins the boxes of some text into one per line.
function lineBoxes(ranges: Range[]) {
  const out: { left: number; top: number; right: number; bottom: number }[] = [];
  for (const c of ranges.flatMap((r) => Array.from(r.getClientRects()))) {
    if (c.width <= 0 || c.height <= 0) continue;
    const line = out.find((l) => Math.abs(l.top - c.top) < c.height / 2);
    if (line) {
      line.left = Math.min(line.left, c.left); line.right = Math.max(line.right, c.right);
      line.top = Math.min(line.top, c.top); line.bottom = Math.max(line.bottom, c.bottom);
    } else out.push({ left: c.left, top: c.top, right: c.right, bottom: c.bottom });
  }
  return out.map((l) => ({ left: l.left, top: l.top, width: l.right - l.left, height: l.bottom - l.top }));
}

function NoteEditor({ draft, onDraft, canClear, onSave, onCancel }: { draft: string; onDraft: (s: string) => void; canClear: boolean; onSave: () => void; onCancel: () => void }) {
  return (
    <div className="ann-editor">
      <textarea className="note-input" autoFocus rows={4} placeholder="写点什么…" aria-label="笔记" value={draft}
        onChange={(e) => onDraft(e.target.value)}
        onKeyDown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); onSave(); } }} />
      <div className="note-actions">
        <span className="hint">⌘/Ctrl + Enter 保存</span>
        <button type="button" className="ann-btn" onClick={onCancel}>取消</button>
        <button type="button" className="ann-btn primary" disabled={!canClear && !draft.trim()} onClick={onSave}>保存</button>
      </div>
    </div>
  );
}

// AnalyzeButton is 分析 on a wide screen. While the assistant is not set
// up it stays, greyed out, and says why when pointed at or focused; it is
// aria-disabled rather than disabled, which would take the pointer away.
function AnalyzeButton({ unavailable, onAnalyze }: { unavailable: string; onAnalyze: () => void }) {
  const tip = useId();
  if (!unavailable) return <button type="button" className="ann-btn analyze" onClick={onAnalyze}><Sparkles />分析</button>;
  return (
    <span className="ann-tip-host">
      <button type="button" className="ann-btn analyze" aria-disabled="true" aria-describedby={tip}><Sparkles />分析</button>
      <span className="ann-tip" id={tip} role="tooltip">{unavailable}</span>
    </span>
  );
}

function Looks({ current, onPick }: { current?: string; onPick: (look: Look) => void }) {
  return (
    <span className="ann-looks">
      <button type="button" className="ann-btn" aria-pressed={current === 'underline'} onClick={() => onPick({ style: 'underline' })}><Underline />横线</button>
      <span className="ann-label">荧光笔</span>
      {HIGHLIGHT_COLORS.map((c) => (
        <button key={c} type="button" className={cn('ann-swatch', `c-${c}`)} aria-pressed={current === c} aria-label={`荧光笔 ${COLOR_LABEL[c]}`} title={COLOR_LABEL[c]}
          onClick={() => onPick({ style: 'highlight', color: c })} />
      ))}
    </span>
  );
}

// Touch screens dock the bar to the bottom of the screen: there the
// browser's own menu for a selection (copy, share, select all…) takes the
// space above it, where a floating bar would go, and covers it.
const DOCK_MEDIA = '(pointer: coarse)';

// Floating sits above the spot, or below it when there is no room above; on
// a touch screen it docks to the bottom of the screen instead, above the
// keyboard while one is up and above the page's bottom bar, if any.
export function Floating({ spot, ref, label, stacked, children }: { spot: Spot; ref: RefObject<HTMLDivElement | null>; label: string; stacked?: boolean; children: ReactNode }) {
  const docked = useMedia(DOCK_MEDIA);
  const keyboard = useKeyboardInset(docked);
  const [below, setBelow] = useState(false);
  const [shift, setShift] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el || docked) return;
    const h = el.offsetHeight, w = el.offsetWidth;
    setBelow(spot.top - scrollY - h - 10 < 64);
    const left = spot.x - scrollX - w / 2;
    setShift(left < 8 ? 8 - left : (left + w > innerWidth - 8 ? innerWidth - 8 - (left + w) : 0));
  }, [spot, ref, children, docked]);
  const style: CSSProperties = docked
    ? (keyboard ? { '--keyboard': `${keyboard}px` } as CSSProperties : {})
    : { left: spot.x + shift, top: below ? spot.bottom + 8 : spot.top - 8, transform: below ? 'translateX(-50%)' : 'translate(-50%, -100%)' };
  return createPortal(
    // clicks keep the selection, except in the note's own field
    <div ref={ref} role="toolbar" aria-label={label} className={cn('ann-bar', stacked && 'stacked', docked && 'docked', docked && keyboard > 0 && 'over-keyboard')}
      onMouseDown={(e) => { if (!(e.target instanceof HTMLTextAreaElement)) e.preventDefault(); }} style={style}>
      {children}
    </div>,
    // in the shell, a docked bar reads the height of the page's bottom bar
    (docked && document.querySelector('.shell')) || document.body,
  );
}
