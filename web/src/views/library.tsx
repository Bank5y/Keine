import { useEffect, useState } from 'react';
import { Search } from 'lucide-react';
import { Dialog as D, HoverCard } from 'radix-ui';
import { Empty, ErrorBox, PageHead } from '@/components/common';
import { CourseList, CourseRow } from '@/components/course-row';
import { Facet, facetItems } from '@/components/facet';
import { FilterBar, FilterButton } from '@/components/filter-button';
import { Peek } from '@/components/peek';
import { CourseCard } from '@/components/shelf';
import { api } from '@/lib/api';
import { COMPLETION_OPTIONS, COURSE_TYPE_OPTIONS } from '@/lib/course-filter';
import { libraryParams, readLibraryFilter } from '@/lib/library-filter';
import { setParam, useSearch } from '@/lib/router';
import { useNarrow } from '@/lib/use-narrow';
import type { CourseSummary } from '@/lib/courses';

const canHover = typeof window !== 'undefined' && window.matchMedia?.('(hover: hover) and (pointer: fine)').matches;

export function LibraryView() {
  const narrow = useNarrow();
  // the filter lives in the address (?type=&completion=&q=), so a search
  // typed in the top bar on any page, or a shared link, opens the library
  // on the same results
  const { type, completion, q: text } = readLibraryFilter(useSearch());
  const [q, setQ] = useState(() => text.trim());
  const [courses, setCourses] = useState<CourseSummary[] | null>(null);
  const [err, setErr] = useState('');
  const [touched, setTouched] = useState<CourseSummary | null>(null);

  useEffect(() => { const t = setTimeout(() => setQ(text.trim()), 250); return () => clearTimeout(t); }, [text]);
  useEffect(() => {
    api<{ courses: CourseSummary[] }>('GET', '/api/library?' + libraryParams({ type, completion, q })).then((r) => { setCourses(r.courses); setErr(''); }).catch((e) => setErr(e.message));
  }, [type, completion, q]);

  const filtered = !!(type || completion || q);
  return (
    <div className="page enter">
      {narrow ? <PageHead title="课程库" /> : (
        <>
          <PageHead kicker="Library" title="课程库" sub="所有人都能学习的公开课程。把鼠标移到课程上, 先看看介绍和目录。" />
          <div className="searchbar">
            <Search />
            <input placeholder="搜索课程名或讲师…" aria-label="搜索课程" value={text} onChange={(e) => setParam('q', e.target.value)} />
          </div>
          <div className="facets">
            <Facet label="类型" value={type} onPick={(v) => setParam('type', v)} items={facetItems(COURSE_TYPE_OPTIONS)} />
            <Facet label="状态" value={completion} onPick={(v) => setParam('completion', v)} items={facetItems(COMPLETION_OPTIONS)} />
          </div>
          <div className="result-bar"><span className="count">{courses ? <><b>{courses.length}</b>门课程</> : '正在整理书架…'}</span></div>
        </>
      )}
      {narrow ? (
        <FilterBar count={courses?.length ?? null} unit="门课程">
          <FilterButton label="类型" value={type} options={COURSE_TYPE_OPTIONS} onChange={(v) => setParam('type', v)} />
          <FilterButton label="状态" value={completion} options={COMPLETION_OPTIONS} onChange={(v) => setParam('completion', v)} />
        </FilterBar>
      ) : null}
      {err ? <ErrorBox message={err} /> : null}
      {courses && !courses.length ? <Empty glyph={filtered ? '无' : '空'} title={filtered ? '没有符合条件的课程' : '课程库还是空的'}><div>{filtered ? '换个关键词或筛选条件试试' : '公开的课程会出现在这里'}</div></Empty> : null}
      {courses && courses.length && narrow ? (
        <CourseList>
          {courses.map((c) => (
            <CourseRow key={c.id} course={c} href={`/courses/${c.id}`} mark={q}>
              {c.subscribed ? <span className="tag subscribed">已订阅</span> : null}
            </CourseRow>
          ))}
        </CourseList>
      ) : null}
      {courses && courses.length && !narrow ? (
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
