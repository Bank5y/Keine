// PROTOTYPE (#137): mock exploration dialogue shared by every variant. A
// node is a line of turns; every node but the main one forks from a turn of
// its parent (ADR 0009). In-memory only.

export type NodeType = 'main' | 'child' | 'branch' | 'divergent';

export interface Turn { id: string; q: string; a: string; fragment?: string }
export interface DNode {
  id: string; type: NodeType; name: string;
  parent?: string; forkTurn?: string;
  turns: Turn[];
}

export const TYPES: Record<NodeType, { label: string; color: string; soft: string; verb: string; hint: string }> = {
  main: { label: '主节点', color: '#4b6a9b', soft: '#e6ecf4', verb: '主线', hint: '' },
  child: { label: '子节点', color: '#6e8bb8', soft: '#e9eef6', verb: '追问', hint: '沿这一轮继续追问，往深处挖' },
  branch: { label: '分支节点', color: '#5e8a6a', soft: '#e4eee7', verb: '扩展', hint: '在课程里拓宽这个概念' },
  divergent: { label: '发散节点', color: '#b07a3a', soft: '#f4ebdf', verb: '发散', hint: '走出课程，看同类的其他内容' },
};

export const DEFAULT_Q: Record<Exclude<NodeType, 'main'>, string> = {
  child: '沿着这一点继续往下讲：它具体是怎么做到的，为什么要这样？',
  branch: '在这门课里，和它相关的还有哪些概念？它们和它是什么关系？',
  divergent: '课程之外，还有哪些同类的做法？和这里的做法相比有什么异同？',
};

export const QUOTE = { text: '所有的控制流都是显式的', from: '01｜用户输入规范化' };
export const TITLE = 'Agent（智能体）';
export const INTRO = '能够自主规划步骤、调用工具，并根据执行结果调整下一步行动的大模型应用。';

const initial: DNode[] = [
  {
    id: 'main', type: 'main', name: '主节点',
    turns: [
      { id: 'm1', q: '分析「所有的控制流都是显式的」', a: `**${TITLE}**：${INTRO}\n\n在这一讲里，Agent 指文中那个最小的 run 函数：planner 负责规划，executor 逐步执行，出错时按约定重试。` },
      { id: 'm2', q: 'planner 拆出来的步骤，executor 是怎么知道每一步该调哪个工具的？', a: '在文中的最小实现里，planner 的输出本身就带着工具名：每一步是 {tool, args}，executor 只负责按名字找到工具并执行，不再做选择。这样选择权集中在规划阶段，执行阶段没有分支。\n\n代价是 planner 必须在开始前就知道全部工具。' },
      { id: 'm3', q: '如果 planner 一开始就规划错了，executor 能发现吗？', a: '能发现一部分。executor 只能看到「这一步执行失败了」，看不到「这一步本来就不该做」。所以文中把失败分成两类：工具报错时原地重试；连续失败或结果不符合预期时，把已执行的步骤和结果交回 planner 重新规划。' },
      { id: 'm4', q: '重新规划会不会把已经做过的步骤再做一遍？', a: '不会。交回 planner 的是「已完成步骤 + 结果」，planner 只规划剩下的部分。文中用一个 done 列表记录，重新规划时作为上下文的一部分。' },
    ],
  },
  {
    id: 'c1', type: 'child', name: '选择权集中在规划阶段', parent: 'main', forkTurn: 'm2',
    turns: [
      { id: 'c1t1', fragment: '选择权集中在规划阶段', q: DEFAULT_Q.child, a: '因为 executor 拿到的每一步都已经写明了用哪个工具、传什么参数，它不需要再判断。判断只发生一次，在 planner 产出计划的时候。好处是执行过程可以完整重放。' },
      { id: 'c1t2', q: '那计划本身用什么格式写比较好？', a: '文中用的是一个 JSON 数组，每一项是 {tool, args, why}。why 不给 executor 用，而是在失败时交回 planner，帮它理解当初为什么这么规划。' },
    ],
  },
  {
    id: 'c2', type: 'child', name: 'why 字段', parent: 'c1', forkTurn: 'c1t2',
    turns: [
      { id: 'c2t1', fragment: 'why', q: 'why 字段会不会让计划变得很长、很贵？', a: '会多一些 token，但 why 通常只有一句话。文中的取舍是：失败时能少一次「重新理解目标」的调用，整体更省。' },
    ],
  },
  {
    id: 'c3', type: 'child', name: '一句话的 why 够不够', parent: 'c2', forkTurn: 'c2t1',
    turns: [
      { id: 'c3t1', q: '一句话的 why 真的够 planner 重新理解吗？', a: '对简单任务够。复杂任务里，planner 重新规划时还会看到原始目标和 done 列表，why 只是补充当时的意图。' },
    ],
  },
  {
    id: 'c4', type: 'child', name: '原始目标放在哪', parent: 'c3', forkTurn: 'c3t1',
    turns: [
      { id: 'c4t1', fragment: '原始目标', q: '原始目标是每次重新规划都整段带上吗？', a: '是的，原始目标放在系统提示词之后、done 列表之前，每次重新规划都带上，这部分前缀不变，可以命中缓存。' },
    ],
  },
  {
    id: 'c5', type: 'divergent', name: '提示词缓存', parent: 'c4', forkTurn: 'c4t1',
    turns: [
      { id: 'c5t1', fragment: '命中缓存', q: DEFAULT_Q.divergent, a: '课程之外，各家服务商都有前缀缓存：Anthropic 要显式加缓存标记，DeepSeek 和 OpenAI 自动按前缀命中。共同点是前缀必须逐字相同。' },
    ],
  },
  {
    id: 'b1', type: 'branch', name: 'ReAct', parent: 'main', forkTurn: 'm2',
    turns: [
      { id: 'b1t1', q: DEFAULT_Q.branch, a: '课程第 03 讲的 ReAct 是另一种做法：不先出完整计划，而是每一步「想一下 → 做一步 → 看结果」。它和这里的 planner/executor 是两个极端：一个把选择全放在开头，一个把选择分散到每一步。' },
    ],
  },
  {
    id: 'b2', type: 'branch', name: '记忆', parent: 'main', forkTurn: 'm4',
    turns: [
      { id: 'b2t1', fragment: 'done 列表', q: DEFAULT_Q.branch, a: 'done 列表其实是最简单的短期记忆。第 04 章「记忆」会把它推广成可检索的长期记忆，用来跨任务复用经验。' },
    ],
  },
  {
    id: 'd1', type: 'divergent', name: '工作流引擎', parent: 'main', forkTurn: 'm3',
    turns: [
      { id: 'd1t1', q: '课程之外，工作流引擎（比如 Temporal）也是先规划后执行吗？', a: 'Temporal 的工作流是代码写死的，没有「规划」这一步，但它的重试、补偿和持久化执行，和这里 executor 的失败处理是同一类问题。' },
      { id: 'd1t2', q: 'Temporal 的补偿和这里的重新规划有什么区别？', a: '补偿是撤销已做的步骤（saga），重新规划是保留已做的步骤、换一条路继续。前者假设路线是对的、只是某步失败；后者承认路线本身可能错了。' },
    ],
  },
  {
    id: 'd2', type: 'branch', name: 'Saga 模式', parent: 'd1', forkTurn: 'd1t2',
    turns: [
      { id: 'd2t1', fragment: 'saga', q: DEFAULT_Q.branch, a: '（课程里没有直接讲 saga；第 03 讲的「回退路径」最接近，它要求每个有副作用的工具都声明怎么撤销。）' },
    ],
  },
];

export function makeStore() {
  return structuredClone(initial);
}

export const byId = (nodes: DNode[], id: string) => nodes.find((n) => n.id === id)!;
export const childrenOf = (nodes: DNode[], id: string) => nodes.filter((n) => n.parent === id);
export const forksAt = (nodes: DNode[], turnId: string) => nodes.filter((n) => n.forkTurn === turnId);

// the turns a node sees before its own: its ancestors', each cut at the fork
export function inherited(nodes: DNode[], id: string): { node: DNode; turns: Turn[] }[] {
  const out: { node: DNode; turns: Turn[] }[] = [];
  let n = byId(nodes, id);
  while (n.parent) {
    const p = byId(nodes, n.parent);
    const cut = p.turns.findIndex((t) => t.id === n.forkTurn);
    out.unshift({ node: p, turns: p.turns.slice(0, cut + 1) });
    n = p;
  }
  return out;
}

export function path(nodes: DNode[], id: string): DNode[] {
  const out: DNode[] = [];
  let n: DNode | undefined = byId(nodes, id);
  while (n) { out.unshift(n); n = n.parent ? byId(nodes, n.parent) : undefined; }
  return out;
}

export const turnIndex = (nodes: DNode[], turnId: string) => {
  for (const n of nodes) { const i = n.turns.findIndex((t) => t.id === turnId); if (i >= 0) return { node: n, i }; }
  return null;
};

export const depth = (nodes: DNode[], id: string) => path(nodes, id).length - 1;

let seq = 0;
export function addNode(nodes: DNode[], type: Exclude<NodeType, 'main'>, parent: string, forkTurn: string, q: string, fragment?: string): DNode[] {
  const id = `n${++seq}`;
  const name = fragment || (q.length > 14 ? q.slice(0, 14) + '…' : q);
  const node: DNode = { id, type, name, parent, forkTurn, turns: [{ id: `${id}t1`, q, fragment, a: '（原型：这里是助手的回复。）' }] };
  return [...nodes, node];
}

export function removeNode(nodes: DNode[], id: string): DNode[] {
  const gone = new Set([id]);
  let grew = true;
  while (grew) { grew = false; for (const n of nodes) if (n.parent && gone.has(n.parent) && !gone.has(n.id)) { gone.add(n.id); grew = true; } }
  return nodes.filter((n) => !gone.has(n.id));
}
