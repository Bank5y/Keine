import { describe, expect, it } from 'vitest';
import { unavailableText } from '@/lib/ai';

describe('unavailableText', () => {
  it('tells a reader only that the admin has to set it up', () => {
    expect(unavailableText({ reason: 'search_unrouted', model: 'Qwen' }, false)).toBe('AI 暂不可用：管理员还没配置好模型服务商');
  });

  it('tells the admin what to fix and where', () => {
    expect(unavailableText({ reason: 'turns_unmapped', turns: ['followup', 'sync'] }, true)).toBe('AI 未启用：后续轮、同步还没选模型。在「管理 → 模型 → 轮次」里配置。');
    expect(unavailableText({ reason: 'search_unrouted', model: 'Qwen' }, true)).toBe('AI 未启用：Qwen 没有可用的联网路线。在「管理 → 模型 → 联网」里配置。');
  });

  it('falls back for a reason it does not know', () => {
    expect(unavailableText({ reason: 'something_new' }, true)).toBe('AI 未启用。在「管理 → 模型」里检查配置。');
    expect(unavailableText(undefined, true)).toBe('AI 未启用。在「管理 → 模型」里检查配置。');
  });
});
