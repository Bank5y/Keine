import { useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import { Copy, NotebookPen, Pencil, Trash2, Underline } from 'lucide-react';
import { toast } from 'sonner';
import { api, ApiError } from '@/lib/api';
import { ANNOTATION_HASH, COLOR_LABEL, HIGHLIGHT_COLORS, TextMap, lookName, type Annotation, type Look, type Quote } from '@/lib/annotations';
import { useKeyboardInset } from '@/lib/use-keyboard-inset';
import { claimTap } from '@/lib/use-lesson-bar';
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
// the gap between stacked note cards
const CARD_GAP = 8;

const failed = (ex: unknown) => toast.error(ex instanceof ApiError ? ex.message : '操作失败');

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success('已复制');
  } catch {
    toast.error('复制失败');
  }
}

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
  const [pick, setPick] = useState<{ spot: Spot; quote: Quote; range: Range } | null>(null);
  const [open, setOpen] = useState<{ spot: Spot; ids: number[] } | null>(null);
  const [editing, setEditing] = useState<Editing | null>(null);
  // the annotation whose note card is picked, or whose text was clicked
  const [active, setActive] = useState<number | null>(null);
  const bar = useRef<HTMLDivElement>(null);
  const wide = useMedia(NOTE_RAIL_MEDIA);
  const railed = wide && !!rail;

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
  const focused = editing ? ('id' in editing ? placed.get(editing.id) : editing.ranges) : (active != null ? placed.get(active) : undefined);
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
    // a tap on the bar may clear the selection on a touch screen before the
    // tap lands; the bar stays for it
    let onBar = -Infinity;
    const read = () => {
      const s = getSelection();
      if (!s || s.isCollapsed || !s.rangeCount) { setPick(null); return; }
      const range = s.getRangeAt(0);
      const quote = root.contains(range.commonAncestorContainer) ? map.quote(range) : null;
      if (!quote) { setPick(null); return; }
      setOpen(null);
      setEditing(null);
      setPick({ spot: spotOf(range.getBoundingClientRect()), quote, range: range.cloneRange() });
    };
    const onChange = () => { clearTimeout(t); if (!down && performance.now() - onBar > 600) t = window.setTimeout(read, 120); };
    const onDown = (e: PointerEvent) => { if (bar.current?.contains(e.target as Node)) onBar = performance.now(); else down = true; };
    const onUp = () => { if (down) { down = false; clearTimeout(t); t = window.setTimeout(read, 0); } };
    document.addEventListener('selectionchange', onChange);
    document.addEventListener('pointerdown', onDown);
    document.addEventListener('pointerup', onUp);
    return () => {
      clearTimeout(t);
      document.removeEventListener('selectionchange', onChange);
      document.removeEventListener('pointerdown', onDown);
      document.removeEventListener('pointerup', onUp);
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
      if (hits.length) claimTap(e);
      setEditing(null);
      setOpen(hits.length && at ? { spot: { x: e.clientX + scrollX, top: at.top + scrollY, bottom: at.bottom + scrollY }, ids: hits } : null);
      setActive(hits.find((id) => anns.find((a) => a.id === id)?.note) ?? null);
    };
    root.addEventListener('click', onClick);
    return () => root.removeEventListener('click', onClick);
  }, [root, anns, placed]);

  // an open bar closes on a click elsewhere or on Escape; so does the focus
  // on a card
  useEffect(() => {
    if (!open && !editing && active == null) return undefined;
    const inside = (t: Node) => bar.current?.contains(t) || root?.contains(t) || (t instanceof Element && t.closest('.note-card'));
    const onDown = (e: PointerEvent) => {
      if (inside(e.target as Node)) return;
      setOpen(null); setEditing(null); setActive(null);
    };
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') { setOpen(null); setEditing(null); setActive(null); } };
    document.addEventListener('pointerdown', onDown);
    document.addEventListener('keydown', onKey);
    return () => { document.removeEventListener('pointerdown', onDown); document.removeEventListener('keydown', onKey); };
  }, [open, editing, active, root]);

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

  // startNote opens the note editor on an annotation, at its text
  const startNote = (a: Annotation) => {
    const line = firstLine(placed.get(a.id) ?? []);
    const spot = open?.spot ?? (line ? spotOf(line) : null);
    if (!spot) return;
    setOpen(null);
    setEditing({ spot, id: a.id, draft: a.note ?? '' });
  };

  let floating: ReactNode = null;
  if (editing) {
    const isNew = 'quote' in editing;
    floating = (
      <Floating spot={editing.spot} ref={bar} label={isNew ? '写笔记' : '编辑笔记'} stacked>
        {isNew ? <div className="ann-row"><span className="ann-looks"><Looks current={editing.look.style === 'underline' ? 'underline' : editing.look.color} onPick={(look) => setEditing({ ...editing, look })} /></span></div> : null}
        <NoteEditor draft={editing.draft} onDraft={(draft) => setEditing({ ...editing, draft })} canClear={!isNew}
          onSave={() => { if (isNew) void create(editing.look, editing.draft.trim() ? editing.draft : undefined); else void writeNote(editing.id, editing.draft); }}
          onCancel={() => setEditing(null)} />
      </Floating>
    );
  } else if (pick) {
    floating = (
      <Floating spot={pick.spot} ref={bar} label="标注">
        <span className="ann-looks"><Looks onPick={(look) => create(look)} /></span>
        <span className="ann-sep" />
        <span className="ann-acts">
          <button type="button" className="ann-btn" onClick={() => { setEditing({ spot: pick.spot, quote: pick.quote, ranges: [pick.range], look: { style: 'highlight', color: 'yellow' }, draft: '' }); setPick(null); }}><NotebookPen />写笔记</button>
          <button type="button" className="ann-btn" onClick={() => { void copy(pick.quote.quote); }}><Copy />复制</button>
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
              <span className="ann-looks"><Looks current={lookName(a)} onPick={(look) => restyle(a.id, look)} /></span>
              <span className="ann-sep" />
              <span className="ann-acts">
                {a.note ? null : <button type="button" className="ann-btn" onClick={() => startNote(a)}><NotebookPen />写笔记</button>}
                <button type="button" className="ann-btn" onClick={() => { void copy(a.quote); }}><Copy />复制</button>
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
        <NoteRail rail={rail} root={root} anns={anns} placed={placed} active={editing && 'id' in editing ? editing.id : active}
          onPick={(id) => { setOpen(null); setEditing(null); setActive(id); }}
          onEdit={startNote} onDelete={(a) => writeNote(a.id, '')} />
      ) : null}
      {floating}
    </>
  );
}

// NoteRail lays the notes out in the rail: each card level with the first
// line of its annotation, pushed down below the one before where they would
// overlap. Annotations without a note, or not found in the text, take no room.
function NoteRail({ rail, root, anns, placed, active, onPick, onEdit, onDelete }: {
  rail: HTMLElement; root: HTMLElement | null; anns: Annotation[]; placed: Map<number, Range[]>; active: number | null;
  onPick: (id: number) => void; onEdit: (a: Annotation) => void; onDelete: (a: Annotation) => void;
}) {
  const noted = useMemo(() => anns.filter((a) => a.note && placed.has(a.id)), [anns, placed]);
  const cards = useRef(new Map<number, HTMLElement>());
  const [tops, setTops] = useState<Map<number, number>>(new Map());
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
    const want = noted.map((a) => ({ a, top: (firstLine(placed.get(a.id) ?? [])?.top ?? base) - base }))
      .sort((x, y) => x.top - y.top || (x.a.range?.start ?? 0) - (y.a.range?.start ?? 0));
    const next = new Map<number, number>();
    let end = -Infinity;
    for (const { a, top } of want) {
      const at = Math.max(top, end + CARD_GAP);
      next.set(a.id, at);
      end = at + (cards.current.get(a.id)?.offsetHeight ?? 0);
    }
    setTops((prev) => (prev.size === next.size && [...next].every(([id, t]) => Math.abs((prev.get(id) ?? NaN) - t) < 0.5) ? prev : next));
  }, [rail, noted, placed, active, tick]);

  return createPortal(
    noted.map((a) => (
      <div key={a.id} ref={(el) => { if (el) cards.current.set(a.id, el); else cards.current.delete(a.id); }}
        className={cn('note-card', a.id === active && 'active')} data-annotation={a.id} role="button" tabIndex={0}
        aria-pressed={a.id === active} aria-label={`笔记：${a.quote}`}
        style={{ top: tops.get(a.id) ?? 0, visibility: tops.has(a.id) ? undefined : 'hidden' }}
        onClick={() => onPick(a.id)} onKeyDown={(e) => { if (e.key === 'Enter' && e.target === e.currentTarget) onPick(a.id); }}>
        <div className={cn('note-quote', `look-${lookName(a)}`)}>{a.quote}</div>
        <p className="note-text">{a.note}</p>
        <div className="note-actions">
          <button type="button" className="ann-btn" aria-label="编辑笔记" title="编辑笔记" onClick={(e) => { e.stopPropagation(); onEdit(a); }}><Pencil /></button>
          <button type="button" className="ann-btn danger" aria-label="删除笔记" title="删除笔记" onClick={(e) => { e.stopPropagation(); onDelete(a); }}><Trash2 /></button>
        </div>
      </div>
    )),
    rail,
  );
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

function Looks({ current, onPick }: { current?: string; onPick: (look: Look) => void }) {
  return (
    <>
      <button type="button" className="ann-btn" aria-pressed={current === 'underline'} onClick={() => onPick({ style: 'underline' })}><Underline />横线</button>
      <span className="ann-label">荧光笔</span>
      {HIGHLIGHT_COLORS.map((c) => (
        <button key={c} type="button" className={cn('ann-swatch', `c-${c}`)} aria-pressed={current === c} aria-label={`荧光笔 ${COLOR_LABEL[c]}`} title={COLOR_LABEL[c]}
          onClick={() => onPick({ style: 'highlight', color: c })} />
      ))}
    </>
  );
}

// Touch screens dock the bar to the bottom of the screen: there the
// browser's own menu for a selection (copy, share, select all…) takes the
// space above it, where a floating bar would go, and covers it.
export const DOCK_MEDIA = '(pointer: coarse)';

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
