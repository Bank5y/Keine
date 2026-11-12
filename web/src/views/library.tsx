import { useEffect, useState } from 'react';
import { Search } from 'lucide-react';
import { Dialog as D, HoverCard } from 'radix-ui';
import { Empty, ErrorBox, PageHead } from '@/components/common';
import { Facet } from '@/components/facet';
import { Peek } from '@/components/peek';
import { CourseCard } from '@/components/shelf';
import { api } from '@/lib/api';
import { COMPLETION_LABEL, TYPE_LABEL, type CourseSummary } from '@/lib/courses';

const canHover = typeof window !== 'undefined' && window.matchMedia?.('(hover: hover) and (pointer: fine)').matches;

export function LibraryView() {
  const [type, setType] = useState('');
  const [completion, setCompletion] = useState('');
  const [text, setText] = useState('');
  const [q, setQ] = useState('');
  const [courses, setCourses] = useState<CourseSummary[] | null>(null);
  const [err, setErr] = useState('');
  const [touched, setTouched] = useState<CourseSummary | null>(null);

  useEffect(() => { const t = setTimeout(() => setQ(text.trim()), 250); return () => clearTimeout(t); }, [text]);
  useEffect(() => {
    const p = new URLSearchParams();
    if (type) p.set('type', type);
    if (completion) p.set('completion', completion);
    if (q) p.set('q', q);
    api<{ courses: CourseSummary[] }>('GET', '/api/library?' + p).then((r) => { setCourses(r.courses); setErr(''); }).catch((e) => setErr(e.message));
  }, [type, completion, q]);

  const filtered = !!(type || completion || q);
  return (
    <div className="page enter">
      <PageHead kicker="Library" title="课程库" sub="所有人都能学习的公开课程。把鼠标移到课程上, 先看看介绍和目录。" />
      <div className="searchbar">
        <Search />
        <input placeholder="搜索课程名或讲师…" aria-label="搜索课程" value={text} onChange={(e) => setText(e.target.value)} />
      </div>
      <div className="facets">
        <Facet label="类型" value={type} onPick={setType} items={[['', '全部'], ['column', TYPE_LABEL.column], ['video', TYPE_LABEL.video]]} />
        <Facet label="状态" value={completion} onPick={setCompletion} items={[['', '全部'], ['finished', COMPLETION_LABEL.finished], ['ongoing', COMPLETION_LABEL.ongoing], ['incomplete', COMPLETION_LABEL.incomplete]]} />
      </div>
      <div className="result-bar"><span className="count">{courses ? <><b>{courses.length}</b>门课程</> : '正在整理书架…'}</span></div>
      {err ? <ErrorBox message={err} /> : null}
      {courses && !courses.length ? <Empty glyph={filtered ? '无' : '空'} title={filtered ? '没有符合条件的课程' : '课程库还是空的'}><div>{filtered ? '换个关键词或筛选条件试试' : '公开的课程会出现在这里'}</div></Empty> : null}
      {courses && courses.length ? (
        <div className="shelf">
          {courses.map((c) => (canHover ? (
            <HoverCard.Root key={c.id} openDelay={350} closeDelay={150}>
              <HoverCard.Trigger asChild><div><CourseCard course={c} href={`/courses/${c.id}`} /></div></HoverCard.Trigger>
              <HoverCard.Portal>
                <HoverCard.Content side="right" align="start" sideOffset={12} collisionPadding={{ top: 76, bottom: 12, left: 12, right: 12 }} className="z-30 data-[state=open]:animate-in data-[state=open]:fade-in-0">
                  <Peek course={c} />
                </HoverCard.Content>
              </HoverCard.Portal>
            </HoverCard.Root>
          ) : (
            <div key={c.id} onClickCapture={(e) => { e.preventDefault(); e.stopPropagation(); setTouched(c); }}>
              <CourseCard course={c} href={`/courses/${c.id}`} />
            </div>
          )))}
        </div>
      ) : null}
      <D.Root open={!!touched} onOpenChange={(v) => { if (!v) setTouched(null); }}>
        <D.Portal>
          <D.Overlay className="fixed inset-0 z-40 bg-black/40" />
          <D.Content aria-describedby={undefined} className="fixed top-1/2 left-1/2 z-50 -translate-x-1/2 -translate-y-1/2 outline-none">
            <D.Title className="sr-only">{touched?.title}</D.Title>
            {touched ? <Peek course={touched} /> : null}
          </D.Content>
        </D.Portal>
      </D.Root>
    </div>
  );
}
