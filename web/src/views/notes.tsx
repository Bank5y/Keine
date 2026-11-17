import { useEffect, useRef, useState } from 'react';
import { Download, NotebookPen, Pencil, Search, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { Empty, ErrorBox, PageHead } from '@/components/common';
import { Facet, facetItems } from '@/components/facet';
import { FilterBar, FilterButton } from '@/components/filter-button';
import { api, ApiError } from '@/lib/api';
import { COLOR_LABEL, HIGHLIGHT_COLORS, STATE_LABEL, annotationURL, lookName, type AnnotationStyle, type HighlightColor, type NoteEntry, type Notebook, type NoteSort } from '@/lib/annotations';
import { Link } from '@/lib/router';
import { useNarrow } from '@/lib/use-narrow';
import { cn } from '@/lib/utils';

const SEARCH_DELAY_MS = 250;

const styleOptions: { value: AnnotationStyle; label: string }[] = [{ value: 'underline', label: '横线' }, { value: 'highlight', label: '荧光笔' }];
const colorOptions = HIGHLIGHT_COLORS.map((c) => ({ value: c, label: COLOR_LABEL[c] }));
const SORT_LABEL: Record<NoteSort, string> = { time: '按时间', text: '按原文顺序' };
const day = (iso: string) => new Date(iso).toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' }).replace(/-/g, '/');

const failed = (ex: unknown) => toast.error(ex instanceof ApiError ? ex.message : '操作失败');

const when = (iso: string) => new Date(iso).toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });

// NotesView is the notes page: every annotation of the reader's, course by
// course, to filter, search, sort and write notes on; a quote leads back to
// its place in the lesson.
export function NotesView() {
  const [book, setBook] = useState<Notebook | null>(null);
  const [err, setErr] = useState('');
  const [course, setCourse] = useState<number | null>(null);
  const narrow = useNarrow();
  const [style, setStyle] = useState<AnnotationStyle | ''>('');
  const [color, setColor] = useState<HighlightColor | ''>('');
  const [noted, setNoted] = useState(false);
  const [sort, setSort] = useState<NoteSort>('time');
  const [typed, setTyped] = useState('');
  const [q, setQ] = useState('');

  useEffect(() => {
    const t = setTimeout(() => setQ(typed.trim()), SEARCH_DELAY_MS);
    return () => clearTimeout(t);
  }, [typed]);

  useEffect(() => {
    let alive = true;
    const p = new URLSearchParams({ sort });
    if (course != null) p.set('course', String(course));
    if (style) p.set('style', style);
    if (color) p.set('color', color);
    if (noted) p.set('noted', '1');
    if (q) p.set('q', q);
    api<Notebook>('GET', '/api/my/annotations?' + p)
      .then((b) => { if (alive) { setBook(b); setErr(''); } })
      .catch((e) => { if (alive) setErr(e.message); });
    return () => { alive = false; };
  }, [course, style, color, noted, sort, q]);

  const total = book?.courses.reduce((n, c) => n + c.count, 0) ?? 0;
  const filtered = !!(style || color || noted || q);
  const nothingYet = book && !book.courses.length;
  const picked = book?.courses.find((c) => c.id === course);

  const saved = (id: number, note: string | null) =>
    setBook((b) => b && { ...b, annotations: b.annotations.map((a) => (a.id === id ? { ...a, note } : a)) });
  // an underline has no colour, so picking one clears the other
  const pickStyle = (v: AnnotationStyle | '') => { setStyle(v); if (v === 'underline') setColor(''); };
  const pickColor = (c: HighlightColor | '') => { setColor(c); if (c && style === 'underline') setStyle(''); };

  // the whole course, whatever the filter: one .md file, lesson by lesson
  const exportLink = picked && picked.count ? (
    <a className="ann-btn notes-export" href={`/api/my/annotations/export?course=${picked.id}`} download
      title={`把《${picked.title}》的全部标注导出为 Markdown`}><Download aria-hidden="true" />导出 Markdown</a>
  ) : null;
  const list = book && !book.annotations.length ? (
    <Empty glyph="无" title={filtered ? '没有符合条件的标注' : '这门课还没有标注'}>{filtered ? <div>换个筛选条件或搜索词试试</div> : null}</Empty>
  ) : (
    <ol className="note-list">
      {book?.annotations.map((a) => <NoteCardView key={a.id} a={a} onSaved={saved} narrow={narrow} />)}
    </ol>
  );

  return (
    <div className="page enter">
      {narrow ? <PageHead title="笔记" /> : <PageHead kicker="Notes" title="笔记" sub="你在各门课程里做的标注和笔记。点原文回到它在讲里的位置。" />}
      {err ? <ErrorBox message={err} /> : null}
      {nothingYet ? (
        <Empty glyph="札" title="还没有标注">在专栏文章里选中一段文字, 就可以用横线或荧光笔标出, 也可以写笔记。</Empty>
      ) : null}
      {book && !nothingYet && narrow ? (
        <div className="notes-narrow">
          <label className="notes-search">
            <Search aria-hidden="true" />
            <input type="search" placeholder="搜索原文和笔记" aria-label="搜索原文和笔记" value={typed} onChange={(e) => setTyped(e.target.value)} />
          </label>
          {/* the course column folds into 全部课程; style and colour exclude each other as on the desktop */}
          <FilterBar count={null} unit="条标注">
            <FilterButton label="全部课程" value={course == null ? '' : String(course)} options={book.courses.map((c) => ({ value: String(c.id), label: c.title }))}
              onChange={(v) => setCourse(v ? Number(v) : null)} />
            <FilterButton label="样式" value={style} options={styleOptions} onChange={pickStyle} />
            <FilterButton label="颜色" value={color} options={colorOptions} onChange={pickColor} />
          </FilterBar>
          <div className="result-bar">
            <span className="count"><b>{book.annotations.length}</b>条标注</span>
            <span className="notes-tools">
              {exportLink}
              <button type="button" className="notes-sort-toggle" aria-label={`排序：${SORT_LABEL[sort]}，点按切换`}
                onClick={() => setSort(sort === 'time' ? 'text' : 'time')}>{SORT_LABEL[sort]}</button>
            </span>
          </div>
          {list}
        </div>
      ) : null}
      {book && !nothingYet && !narrow ? (
        <div className="notes">
          <nav className="notes-courses" aria-label="课程">
            <button type="button" className={cn('nc', course == null && 'active')} aria-pressed={course == null} onClick={() => setCourse(null)}>
              <span className="t">全部课程</span><span className="n">{total}</span>
            </button>
            {book.courses.map((c) => (
              <button key={c.id} type="button" className={cn('nc', course === c.id && 'active', !c.count && 'none')} aria-pressed={course === c.id} onClick={() => setCourse(c.id)}>
                <span className="t">{c.title}</span><span className="n">{c.count}</span>
              </button>
            ))}
          </nav>

          <div className="notes-main">
            <label className="notes-search">
              <Search aria-hidden="true" />
              <input type="search" placeholder="搜索原文和笔记" aria-label="搜索原文和笔记" value={typed} onChange={(e) => setTyped(e.target.value)} />
            </label>
            <div className="facets">
              <Facet label="样式" value={style} onPick={(v) => pickStyle(v as AnnotationStyle | '')}
                items={facetItems(styleOptions)} />
              <div className="facet">
                <span className="facet-label">颜色</span>
                <div className="facet-items">
                  <button type="button" className={cn('fi', !color && 'active')} onClick={() => setColor('')}>全部</button>
                  {HIGHLIGHT_COLORS.map((c) => (
                    <button key={c} type="button" className={cn('fi swatch-fi', color === c && 'active')} aria-pressed={color === c}
                      onClick={() => pickColor(c)}>
                      <span className={cn('ann-swatch', `c-${c}`)} aria-hidden="true" />{COLOR_LABEL[c]}
                    </button>
                  ))}
                </div>
              </div>
              <Facet label="笔记" value={noted ? 'noted' : ''} onPick={(v) => setNoted(v === 'noted')} items={[['', '全部'], ['noted', '只看有笔记的']]} />
            </div>
            <div className="result-bar">
              <span className="count"><b>{book.annotations.length}</b>条标注</span>
              <span className="notes-tools">
                <span className="notes-sort" role="group" aria-label="排序">
                  <button type="button" className={cn('fi', sort === 'time' && 'active')} aria-pressed={sort === 'time'} onClick={() => setSort('time')}>按时间</button>
                  <button type="button" className={cn('fi', sort === 'text' && 'active')} aria-pressed={sort === 'text'} onClick={() => setSort('text')}>按原文顺序</button>
                </span>
                {exportLink}
              </span>
            </div>
            {list}
          </div>
        </div>
      ) : null}
    </div>
  );
}

// NoteCardView is one annotation: its quote in its look, its note (written
// in place), and where and when it was made.
function NoteCardView({ a, onSaved, narrow }: { a: NoteEntry; onSaved: (id: number, note: string | null) => void; narrow: boolean }) {
  const [draft, setDraft] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const field = useRef<HTMLTextAreaElement>(null);
  useEffect(() => { if (draft != null) field.current?.focus(); }, [draft != null]); // eslint-disable-line react-hooks/exhaustive-deps

  const save = async (note: string) => {
    setBusy(true);
    try {
      const blank = !note.trim();
      if (blank) await api('DELETE', `/api/annotations/${a.id}/note`);
      else await api('PUT', `/api/annotations/${a.id}/note`, { note });
      onSaved(a.id, blank ? null : note);
      setDraft(null);
    } catch (ex) { failed(ex); } finally { setBusy(false); }
  };

  const quote = <span className={cn('nq-text', `look-${lookName(a)}`)}>{a.quote}</span>;
  // the desktop leads with where and when; a narrow card leads with the
  // quote, its look a colour bar beside it, and ends with where and when
  const meta = (
    <div className="nb-meta">
      <span className="nb-where">
        <span className="nb-course">{a.course.title}</span>
        {a.lesson ? <><span className="sep">{narrow ? '·' : '/'}</span><span className="nb-lesson" title={a.lesson.chapter}>{a.lesson.title}</span></> : null}
      </span>
      <span className="nb-badges">
        {a.state !== 'attached' ? <span className={cn('nb-badge', `s-${a.state}`)}>{STATE_LABEL[a.state]}</span> : null}
      </span>
      <time dateTime={a.createdAt} title={narrow ? when(a.createdAt) : undefined}>{narrow ? day(a.createdAt) : when(a.createdAt)}</time>
    </div>
  );
  return (
    <li className={cn('nb-card', narrow && `narrow look-${lookName(a)}`)} data-annotation={a.id}>
      {narrow ? null : meta}
      {a.lesson ? (
        <Link className="nb-quote" href={annotationURL(a.course.id, a.lesson.id, a.id)} title="回到原文">{quote}</Link>
      ) : (
        <div className="nb-quote">{quote}</div>
      )}
      {draft != null ? (
        <div className="nb-editor">
          <textarea ref={field} className="note-input" rows={3} placeholder="写点什么…" aria-label="笔记" value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); void save(draft); }
              if (e.key === 'Escape') setDraft(null);
            }} />
          <div className="note-actions">
            <span className="hint">⌘/Ctrl + Enter 保存</span>
            <button type="button" className="ann-btn" onClick={() => setDraft(null)}>取消</button>
            <button type="button" className="ann-btn primary" disabled={busy || (!a.note && !draft.trim())} onClick={() => void save(draft)}>保存</button>
          </div>
        </div>
      ) : narrow ? (
        // narrow: the note itself is what to tap to edit it; an empty note is
        // written in the lesson, where the quote can be read in context
        a.note ? <button type="button" className="nb-note nb-note-tap" aria-label={`编辑笔记：${a.note}`} onClick={() => setDraft(a.note ?? '')}>{a.note}</button> : null
      ) : a.note ? (
        <div className="nb-note">
          <p className="note-text">{a.note}</p>
          <div className="note-actions">
            <button type="button" className="ann-btn" onClick={() => setDraft(a.note ?? '')}><Pencil />编辑笔记</button>
            <button type="button" className="ann-btn danger" disabled={busy} onClick={() => void save('')}><Trash2 />删除笔记</button>
          </div>
        </div>
      ) : (
        <div className="note-actions nb-add">
          <button type="button" className="ann-btn" onClick={() => setDraft('')}><NotebookPen />写笔记</button>
        </div>
      )}
      {narrow ? meta : null}
    </li>
  );
}
