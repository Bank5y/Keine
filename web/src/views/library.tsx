import { Empty, PageHead } from '@/components/common';

export function LibraryView() {
  return (
    <div className="page enter">
      <PageHead kicker="Library" title="课程库" sub="所有人都能学习的公开课程。" />
      <Empty glyph="空" title="课程库还是空的"><div>公开的课程会出现在这里</div></Empty>
    </div>
  );
}
