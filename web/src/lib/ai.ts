// Why the assistant is unavailable, as the server says (/api/me aiProblem):
// turns without a model, or a model whose search route resolves to nothing.
export interface AIProblem { reason: string; turns?: string[]; model?: string }

const TURN_NAMES: Record<string, string> = { analysis: '分析', followup: '后续轮', sync: '同步' };

// unavailableText is what 分析 says while it is greyed out: to a reader,
// only that the admin has to set it up; to the admin, what to fix where.
export function unavailableText(p: AIProblem | null | undefined, admin: boolean) {
  if (!admin) return 'AI 暂不可用：管理员还没配置好模型服务商';
  switch (p?.reason) {
    case 'turns_unmapped':
      return `AI 未启用：${(p.turns ?? []).map((t) => TURN_NAMES[t] ?? t).join('、')}还没选模型。在「管理 → 模型 → 轮次」里配置。`;
    case 'search_unrouted':
      return `AI 未启用：${p.model} 没有可用的联网路线。在「管理 → 模型 → 联网」里配置。`;
  }
  return 'AI 未启用。在「管理 → 模型」里检查配置。';
}
