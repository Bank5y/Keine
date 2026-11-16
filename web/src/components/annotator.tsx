import { useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import { Copy, Trash2, Underline } from 'lucide-react';
import { toast } from 'sonner';
import { api, ApiError } from '@/lib/api';
import { COLOR_LABEL, HIGHLIGHT_COLORS, TextMap, lookName, type Annotation, type Look, type Quote } from '@/lib/annotations';
import { cn } from '@/lib/utils';

// one CSS highlight per look; underlines paint above highlights so an
// underline inside a highlight stays visible
const LOOKS = ['underline', ...HIGHLIGHT_COLORS] as const;
const highlightName = (look: string) => `ann-${look}`;
const supported = () => typeof CSS !== 'undefined' && 'highlights' in CSS;

const failed = (ex: unknown) => toast.error(ex instanceof ApiError ? ex.message : '操作失败');

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success('已复制');
  } catch {
    toast.error('复制失败');
  }
}

// a spot on the page, in document coordinates, that a floating bar points at
interface Spot { x: number; top: number; bottom: number }
const spotOf = (r: DOMRect): Spot => ({ x: r.left + r.width / 2 + scrollX, top: r.top + scrollY, bottom: r.bottom + scrollY });

// Annotator lets the reader underline and highlight the text of a lesson:
// selecting text offers a toolbar, clicking an annotation offers to restyle
// or delete it (each of several, where they overlap). The annotations are
// painted with the CSS Custom Highlight API, so the rendered Markdown is
// never touched.
export function Annotator({ courseId, lessonId, root }: { courseId: number; lessonId: number; root: HTMLElement | null }) {
  const [anns, setAnns] = useState<Annotation[]>([]);
  const map = useMemo(() => (root ? new TextMap(root) : null), [root]);
  const placed = useMemo(() => {
    const out = new Map<number, Range>();
    if (map) for (const a of anns) { const r = map.place(a); if (r) out.set(a.id, r); }
    return out;
  }, [map, anns]);
  const [pick, setPick] = useState<{ spot: Spot; quote: Quote; range: Range } | null>(null);
  const [open, setOpen] = useState<{ spot: Spot; ids: number[] } | null>(null);
  const bar = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let alive = true;
    api<{ annotations: Annotation[] }>('GET', `/api/courses/${courseId}/lessons/${lessonId}/annotations`)
      .then((r) => { if (alive) setAnns(r.annotations); }).catch(() => {});
    return () => { alive = false; };
  }, [courseId, lessonId]);

  // paint
  useEffect(() => {
    if (!supported()) return undefined;
    for (const look of LOOKS) {
      const h = new Highlight(...anns.filter((a) => lookName(a) === look && placed.has(a.id)).map((a) => placed.get(a.id)!));
      h.priority = look === 'underline' ? 1 : 0;
      CSS.highlights.set(highlightName(look), h);
    }
    return () => { for (const look of LOOKS) CSS.highlights.delete(highlightName(look)); };
  }, [anns, placed]);

  // a finished selection inside the text offers the toolbar
  useEffect(() => {
    if (!map || !root) return undefined;
    let down = false;
    let t = 0;
    const read = () => {
      const s = getSelection();
      if (!s || s.isCollapsed || !s.rangeCount) { setPick(null); return; }
      const range = s.getRangeAt(0);
      const quote = root.contains(range.commonAncestorContainer) ? map.quote(range) : null;
      if (!quote) { setPick(null); return; }
      setOpen(null);
      setPick({ spot: spotOf(range.getBoundingClientRect()), quote, range: range.cloneRange() });
    };
    const onChange = () => { clearTimeout(t); if (!down) t = window.setTimeout(read, 120); };
    const onDown = (e: PointerEvent) => { if (!bar.current?.contains(e.target as Node)) down = true; };
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

  // a click on annotated text opens what lies under it
  useEffect(() => {
    if (!root) return undefined;
    const onClick = (e: MouseEvent) => {
      const s = getSelection();
      if (s && !s.isCollapsed) return;
      if ((e.target as HTMLElement).closest('a, img')) return;
      const hits: number[] = [];
      let at: DOMRect | null = null;
      for (const a of anns) {
        const r = placed.get(a.id);
        const rect = r && Array.from(r.getClientRects()).find((c) => e.clientX >= c.left && e.clientX <= c.right && e.clientY >= c.top && e.clientY <= c.bottom);
        if (rect) { hits.push(a.id); at ??= rect; }
      }
      setOpen(hits.length && at ? { spot: { x: e.clientX + scrollX, top: at.top + scrollY, bottom: at.bottom + scrollY }, ids: hits } : null);
    };
    root.addEventListener('click', onClick);
    return () => root.removeEventListener('click', onClick);
  }, [root, anns, placed]);

  // the open bar closes on a click elsewhere or on Escape
  useEffect(() => {
    if (!open) return undefined;
    const onDown = (e: PointerEvent) => { if (!bar.current?.contains(e.target as Node) && !root?.contains(e.target as Node)) setOpen(null); };
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(null); };
    document.addEventListener('pointerdown', onDown);
    document.addEventListener('keydown', onKey);
    return () => { document.removeEventListener('pointerdown', onDown); document.removeEventListener('keydown', onKey); };
  }, [open, root]);

  const create = async (look: Look) => {
    if (!pick) return;
    try {
      const a = await api<Annotation>('POST', `/api/courses/${courseId}/lessons/${lessonId}/annotations`, { ...look, ...pick.quote });
      setAnns((list) => [...list, a]);
      getSelection()?.removeAllRanges();
      setPick(null);
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
    } catch (ex) { failed(ex); }
  };

  if (pick) {
    return (
      <Floating spot={pick.spot} ref={bar} label="标注">
        <Looks onPick={create} />
        <span className="ann-sep" />
        <button type="button" className="ann-btn" onClick={() => { void copy(pick.quote.quote); }}><Copy />复制</button>
      </Floating>
    );
  }
  if (open) {
    const opened = open.ids.map((id) => anns.find((a) => a.id === id)).filter((a): a is Annotation => !!a);
    return (
      <Floating spot={open.spot} ref={bar} label="编辑标注" stacked={opened.length > 1}>
        {opened.map((a) => (
          <div key={a.id} className="ann-row" data-annotation={a.id}>
            {opened.length > 1 ? <span className="ann-quote" title={a.quote}>{a.quote}</span> : null}
            <Looks current={lookName(a)} onPick={(look) => restyle(a.id, look)} />
            <span className="ann-sep" />
            <button type="button" className="ann-btn" onClick={() => { void copy(a.quote); }}><Copy />复制</button>
            <button type="button" className="ann-btn danger" onClick={() => remove(a.id)}><Trash2 />删除</button>
          </div>
        ))}
      </Floating>
    );
  }
  return null;
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

// Floating sits above the spot, or below it when there is no room above.
function Floating({ spot, ref, label, stacked, children }: { spot: Spot; ref: RefObject<HTMLDivElement | null>; label: string; stacked?: boolean; children: ReactNode }) {
  const [below, setBelow] = useState(false);
  const [shift, setShift] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const h = el.offsetHeight, w = el.offsetWidth;
    setBelow(spot.top - scrollY - h - 10 < 64);
    const left = spot.x - scrollX - w / 2;
    setShift(left < 8 ? 8 - left : (left + w > innerWidth - 8 ? innerWidth - 8 - (left + w) : 0));
  }, [spot, ref]);
  return createPortal(
    <div ref={ref} role="toolbar" aria-label={label} className={cn('ann-bar', stacked && 'stacked')} onMouseDown={(e) => e.preventDefault()}
      style={{ left: spot.x + shift, top: below ? spot.bottom + 8 : spot.top - 8, transform: below ? 'translateX(-50%)' : 'translate(-50%, -100%)' }}>
      {children}
    </div>,
    document.body,
  );
}
