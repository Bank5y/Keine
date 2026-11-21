// Why the assistant is unavailable, as the server says to the admin
// (/api/me aiProblem, the models page's problem): turn kinds without a
// model, or models whose search route resolves to nothing.

export type TurnKind = 'analysis' | 'followup' | 'sync';

// TURN_NAMES are the turn kinds by name.
export const TURN_NAMES: Record<TurnKind, string> = { analysis: '分析', followup: '后续轮', sync: '同步' };

export type AIProblem =
  | { reason: 'turns_unmapped'; turns: TurnKind[] }
  | { reason: 'search_unrouted'; models: string[] };

// unavailableText is what 分析 says while it is greyed out: to a reader,
// only that the admin has to set it up; to the admin, what to fix where.
export function unavailableText(p: AIProblem | null | undefined, admin: boolean) {
  if (!admin) return 'AI 暂不可用：管理员还没配置好模型服务商';
  const why = problemText(p);
  return why ? `AI 未启用：${why}` : 'AI 未启用。在「管理 → 模型」里检查配置。';
}

// problemText says what keeps the assistant off, and where to fix it;
// null for no problem, or one it does not know.
export function problemText(p: AIProblem | null | undefined): string | null {
  switch (p?.reason) {
    case 'turns_unmapped':
      return `${p.turns.map((t) => TURN_NAMES[t] ?? t).join('、')}还没选模型。在「管理 → 模型 → 轮次」里配置。`;
    case 'search_unrouted':
      return `${p.models.join('、')} 没有可用的联网路线。在「管理 → 模型 → 联网」里配置。`;
  }
  return null;
}
