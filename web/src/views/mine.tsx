import { Empty, PageHead } from '@/components/common';

export function MineView() {
  return (
    <div className="page enter">
      <PageHead kicker="My courses" title="我的课程" sub="你正在学习的课程, 以及你上传的私有课程。" />
      <Empty glyph="空" title="还没有课程" />
    </div>
  );
}
