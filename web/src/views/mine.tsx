import { useEffect, useState } from 'react';
import { BookmarkMinus, Ellipsis, FileUp, Trash2, Upload } from 'lucide-react';
import { DropdownMenu as M } from 'radix-ui';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Confirm } from '@/components/confirm';
import { Empty, ErrorBox, PageHead } from '@/components/common';
import { CourseList, CourseRow } from '@/components/course-row';
import { Facet } from '@/components/facet';
import { FilterBar, FilterButton } from '@/components/filter-button';
import { ProgressBar } from '@/components/progress';
import { Shelf } from '@/components/shelf';
import { UploadDialog } from '@/components/upload-dialog';
import { api, ApiError } from '@/lib/api';
import { COMPLETION_LABEL, RETENTION_LABEL, TYPE_LABEL, type CourseSummary } from '@/lib/courses';
import { LIBRARY_COMPLETIONS, LIBRARY_TYPES } from '@/lib/library-filter';
import { MINE_STATES, STUDY_LABEL, mineParams, readMineFilter } from '@/lib/mine-filter';
import { setParam, useSearch } from '@/lib/router';
import { useNarrow } from '@/lib/use-narrow';

const stateOptions = MINE_STATES.map((v) => ({ value: v, label: STUDY_LABEL[v] }));
const typeOptions = LIBRARY_TYPES.map((v) => ({ value: v, label: TYPE_LABEL[v] }));
const completionOptions = LIBRARY_COMPLETIONS.map((v) => ({ value: v, label: COMPLETION_LABEL[v] }));
const facetItems = (o: { value: string; label: string }[]): [string, string][] => [['', '全部'], ...o.map((x): [string, string] => [x.value, x.label])];

// What a reader may do to a course from the list, as the shelf's tools
// allow: leave a library course (or one that has left the library), delete
// a private course of their own.
const canRemove = (c: CourseSummary) => c.visibility !== 'private' || !!c.retention;
const canDelete = (c: CourseSummary) => c.visibility === 'private' && c.isUploader;

export function MineView() {
  const narrow = useNarrow();
  // the filter lives in the address, as the library's does
  const { state, type, completion } = readMineFilter(useSearch());
  const [courses, setCourses] = useState<CourseSummary[] | null>(null);
  const [err, setErr] = useState('');
  const [uploading, setUploading] = useState(false);
  const [deleting, setDeleting] = useState<CourseSummary | null>(null);
  const [removing, setRemoving] = useState<CourseSummary | null>(null);

  const load = () =>
    api<{ courses: CourseSummary[] }>('GET', '/api/my/courses?' + mineParams({ state, type, completion })).then((r) => { setCourses(r.courses); setErr(''); }).catch((e) => setErr(e.message));
  useEffect(() => { void load(); }, [state, type, completion]); // eslint-disable-line react-hooks/exhaustive-deps

  const act = async (method: string, path: string, done: string) => {
    try { await api(method, path); toast.success(done); setDeleting(null); setRemoving(null); void load(); } catch (ex) { toast.error(ex instanceof ApiError ? ex.message : '操作失败'); }
  };
  const filtered = !!(state || type || completion);

  return (
    <div className="page enter">
      {narrow ? (
        <>
          <PageHead title="我的课程" actions={<Button size="sm" onClick={() => setUploading(true)}><FileUp />上传课程</Button>} />
          <FilterBar count={null} unit="门课程">
            <FilterButton label="学习" value={state} options={stateOptions} onChange={(v) => setParam('state', v)} />
            <FilterButton label="类型" value={type} options={typeOptions} onChange={(v) => setParam('type', v)} />
            <FilterButton label="状态" value={completion} options={completionOptions} onChange={(v) => setParam('completion', v)} />
          </FilterBar>
        </>
      ) : (
        <>
          <PageHead
            kicker="My courses"
            title="我的课程"
            sub="你订阅的课程, 以及你上传的私有课程。"
            actions={<Button variant="default" onClick={() => setUploading(true)}><Upload />上传课程</Button>}
          />
          <div className="facets">
            <Facet label="学习" value={state} onPick={(v) => setParam('state', v)} items={facetItems(stateOptions)} />
            <Facet label="类型" value={type} onPick={(v) => setParam('type', v)} items={facetItems(typeOptions)} />
            <Facet label="状态" value={completion} onPick={(v) => setParam('completion', v)} items={facetItems(completionOptions)} />
          </div>
        </>
      )}
      {err ? <ErrorBox message={err} /> : null}
      {courses ? <div className="result-bar"><span className="count"><b>{courses.length}</b>门课程</span><span>按最近学习排序</span></div> : null}
      {courses && !courses.length ? (
        <Empty glyph={filtered ? '无' : '空'} title={filtered ? '没有符合条件的课程' : '还没有课程'}>
          <div>{filtered ? '换个筛选条件试试' : '上传一个课程包, 或去课程库订阅一门课'}</div>
        </Empty>
      ) : null}
      {courses && courses.length && narrow ? (
        <CourseList>
          {courses.map((c) => (
            <CourseRow key={c.id} course={c} href={`/courses/${c.id}`} action={<RowMenu course={c} onRemove={setRemoving} onDelete={setDeleting} />}>
              {c.retention ? <div className="cr-author">{RETENTION_LABEL[c.retention]}</div>
                : c.visibility === 'private' ? <div className="cr-author">私有课程</div> : null}
              <div className="cr-progress">
                <ProgressBar done={c.doneCount} total={c.lessonCount} />
                <span>{c.doneCount} / {c.lessonCount}</span>
              </div>
            </CourseRow>
          ))}
        </CourseList>
      ) : null}
      {courses && courses.length && !narrow ? <Shelf courses={courses} hrefOf={(c) => `/courses/${c.id}`} onDelete={setDeleting} onRemove={setRemoving} showProgress /> : null}
      <UploadDialog open={uploading} onClose={() => setUploading(false)} onUploaded={() => { setUploading(false); void load(); }} />
      <Confirm open={!!deleting} title="删除课程" confirm="删除" onClose={() => setDeleting(null)}
        onConfirm={() => act('DELETE', `/api/courses/${deleting!.id}`, `已删除「${deleting!.title}」`)}>
        确定删除「{deleting?.title}」吗? 删除后无法恢复; 如果有人订阅过它, 他们仍可阅读各自保留的讲 (已学完或有标注的讲)。
      </Confirm>
      <Confirm open={!!removing} title="取消订阅" confirm="取消订阅" onClose={() => setRemoving(null)}
        onConfirm={() => act('DELETE', `/api/courses/${removing!.id}/subscribe`, `已取消订阅「${removing!.title}」`)}>
        {removing?.retention
          ? <>「{removing.title}」{RETENTION_LABEL[removing.retention]}。取消订阅后, 你保留的 {removing.retainedCount ?? 0} 讲会随之失去, 也无法再订阅这门课。</>
          : <>「{removing?.title}」会离开我的课程, 你的学习进度会保留。以后再次订阅, 进度会恢复。</>}
      </Confirm>
    </div>
  );
}

// RowMenu is a course row's ⋯ on a narrow screen: what the reader may do to
// the course, each asking first through the page's confirmations. A course
// with nothing to do has no ⋯.
function RowMenu({ course, onRemove, onDelete }: { course: CourseSummary; onRemove: (c: CourseSummary) => void; onDelete: (c: CourseSummary) => void }) {
  const remove = canRemove(course);
  const del = canDelete(course);
  if (!remove && !del) return null;
  return (
    <M.Root modal={false}>
      <M.Trigger className="row-more" aria-label={`${course.title} 的操作`}><Ellipsis aria-hidden="true" /></M.Trigger>
      <M.Portal>
        <M.Content className="filter-menu" align="end" sideOffset={4} collisionPadding={16}>
          {remove ? <M.Item className="filter-item" onSelect={() => onRemove(course)}><span>取消订阅</span><BookmarkMinus aria-hidden="true" /></M.Item> : null}
          {del ? <M.Item className="filter-item danger" onSelect={() => onDelete(course)}><span>删除课程</span><Trash2 aria-hidden="true" /></M.Item> : null}
        </M.Content>
      </M.Portal>
    </M.Root>
  );
}
