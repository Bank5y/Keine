import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Empty, ErrorBox } from '@/components/common';
import { api, ApiError } from '@/lib/api';
import { indexAction, indexError, indexState, stillBuilding, type IndexedCourse } from '@/lib/concept-index';
import { Link } from '@/lib/router';
import { cn } from '@/lib/utils';

// how often the page looks again while an index is being built
const FOLLOW_MS = 2000;

const at = (s: string) => new Date(s).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });

// AdminConcepts lists the concept index of each library course's current
// version (#158): never built on its own, a new version starts without
// one. Building it extracts the lessons that changed since the last
// version and is charged to the site.
export function AdminConcepts() {
  const [courses, setCourses] = useState<IndexedCourse[] | null>(null);
  const [err, setErr] = useState('');
  const [starting, setStarting] = useState<number | null>(null);

  const load = () => api<{ courses: IndexedCourse[] }>('GET', '/api/admin/concept-indexes').then((r) => { setCourses(r.courses); setErr(''); }).catch((e) => setErr(e.message));
  useEffect(() => { void load(); }, []);
  const following = !!courses && stillBuilding(courses);
  useEffect(() => {
    if (!following) return;
    const t = setInterval(() => void load(), FOLLOW_MS);
    return () => clearInterval(t);
  }, [following]);

  async function build(c: IndexedCourse) {
    setStarting(c.id);
    try {
      await api('POST', `/api/admin/courses/${c.id}/concept-index`);
      toast.success(`开始为「${c.title}」建立概念索引`);
      await load();
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '操作失败');
    } finally {
      setStarting(null);
    }
  }

  return (
    <>
      <p className="tab-intro">概念索引是课程里各个概念和它们所在讲、小节的对照表, 探索对话用它关联课程里的相关内容。索引不会自动建立: 课程发了新版本, 新版本从「未建立」开始; 建立时没改动的讲沿用上一版的条目, 只抽取改动过的讲。费用记在站点上, 不扣读者的积分。</p>
      <div className="result-bar" style={{ marginTop: 0 }}>
        <span className="count">{courses ? <><b>{courses.length}</b>门课程库课程</> : '正在加载…'}</span>
      </div>
      {err ? <ErrorBox message={err} /> : null}
      {courses && !courses.length ? <Empty title="课程库还没有课程">课程公开后, 在这里为它建立概念索引。</Empty> : null}
      {courses?.length ? (
        <div className="ledger concepts">
          {courses.map((c) => {
            const st = indexState(c.status);
            const action = indexAction(c.status);
            return (
              <div className="ledger-row" key={c.id} data-course={c.id}>
                <span className="name"><Link href={`/courses/${c.id}`}>{c.title}</Link></span>
                <span className={cn('st', st.tone)}>{st.label}</span>
                <span className="meta">
                  {c.status === 'built' ? `${c.entries} 个条目` : c.status === 'failed' && c.error ? `原因: ${indexError(c.error)}` : `${c.lessons} 讲`}
                  {' · '}版本 {at(c.updatedAt)}
                </span>
                <span className="acts">
                  {action ? <Button size="sm" variant={c.status === 'none' ? 'default' : 'ghost'} disabled={starting === c.id} onClick={() => void build(c)}>{action}</Button> : null}
                </span>
              </div>
            );
          })}
        </div>
      ) : null}
    </>
  );
}
