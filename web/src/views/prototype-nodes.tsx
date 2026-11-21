// PROTOTYPE (#137): four ways to render an exploration's nodes inside the
// level-2 zoom, switchable via ?variant= on the dev-only /prototype/nodes
// page. Mock data, in memory. See docs/research/branch-rendering.md.
import { PrototypeSwitcher } from '@/components/prototype-switcher';
import { useParam } from '@/lib/router';
import { makeStore } from './prototype-nodes/data';
import { StateReadout } from './prototype-nodes/shell';
import { VariantA, VariantB, VariantC, VariantD, useStore } from './prototype-nodes/variants';

const names = { A: '大纲树 + 单栏对话', B: '线程侧面板', C: '行内标记 + 面包屑', D: '画布' };
const seed = makeStore();

export function PrototypeNodes() {
  const v = (useParam('variant') || 'A') as keyof typeof names;
  const s = useStore(seed);
  return (
    <>
      {v === 'A' && <VariantA s={s} />}
      {v === 'B' && <VariantB s={s} />}
      {v === 'C' && <VariantC s={s} />}
      {v === 'D' && <VariantD s={s} />}
      <StateReadout nodes={s.nodes} current={s.current} />
      <PrototypeSwitcher variants={Object.keys(names)} current={v} names={names} />
    </>
  );
}
