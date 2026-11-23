import { describe, expect, it } from 'vitest';
import { creditLine, emptyOwn, ownBody, ownForm, searchChoices, withKind } from '@/lib/own-model';

describe('searchChoices', () => {
  it('offers the provider’s own search only to an Anthropic model, DeepSeek search only to DeepSeek', () => {
    expect(searchChoices('openai').map((c) => c.id)).toEqual(['off', 'tavily']);
    expect(searchChoices('anthropic').map((c) => c.id)).toEqual(['off', 'provider', 'tavily']);
    expect(searchChoices('deepseek').map((c) => c.id)).toEqual(['off', 'deepseek', 'tavily']);
  });
});

describe('withKind', () => {
  it('fills in DeepSeek’s address, and takes it back out for another kind', () => {
    const ds = withKind(emptyOwn, 'deepseek');
    expect(ds.baseUrl).toBe('https://api.deepseek.com');
    expect(withKind(ds, 'openai').baseUrl).toBe('');
  });

  it('keeps an address the reader typed', () => {
    const typed = { ...emptyOwn, baseUrl: 'https://gateway.example/v1' };
    expect(withKind(typed, 'deepseek').baseUrl).toBe('https://gateway.example/v1');
  });

  it('drops a search the new kind does not offer', () => {
    expect(withKind({ ...emptyOwn, kind: 'anthropic', search: 'provider' }, 'openai').search).toBe('off');
    expect(withKind({ ...emptyOwn, kind: 'openai', search: 'tavily' }, 'anthropic').search).toBe('tavily');
  });
});

describe('ownBody', () => {
  it('sends DeepSeek as the OpenAI format with its provider configuration', () => {
    const f = { ...withKind(emptyOwn, 'deepseek'), key: ' sk-1 ', model: 'deepseek-chat', contextWindow: '' };
    expect(ownBody(f)).toEqual({ format: 'openai', provider: 'deepseek', baseUrl: 'https://api.deepseek.com', key: 'sk-1', model: 'deepseek-chat', contextWindow: null, search: 'off', tavilyKey: '' });
  });

  it('sends the context length as a number, and a Tavily key only for Tavily search', () => {
    const f = { ...emptyOwn, kind: 'anthropic' as const, model: 'claude', contextWindow: '200000', tavilyKey: 'tvly-1' };
    expect(ownBody(f)).toMatchObject({ format: 'anthropic', provider: '', contextWindow: 200000, tavilyKey: '' });
    expect(ownBody({ ...f, search: 'tavily' }).tavilyKey).toBe('tvly-1');
  });
});

describe('ownForm', () => {
  it('reads a saved model back into the form, keys left blank to keep them', () => {
    const f = ownForm({ format: 'openai', provider: 'deepseek', baseUrl: 'https://api.deepseek.com', model: 'deepseek-chat', contextWindow: 64000, search: 'deepseek', keyLast4: 'ab12', tavilyLast4: '', inUse: true });
    expect(f).toEqual({ kind: 'deepseek', baseUrl: 'https://api.deepseek.com', key: '', model: 'deepseek-chat', contextWindow: '64000', search: 'deepseek', tavilyKey: '' });
  });
});

describe('creditLine', () => {
  it('says the run is offline when the reader’s own model reaches no web', () => {
    expect(creditLine('my-model（自带模型）', { enabled: true, inUse: true, offline: true })).toBe('AI 生成 · my-model（自带模型） · 未联网');
    expect(creditLine('Flash', { enabled: true, inUse: false, offline: false })).toBe('AI 生成 · Flash');
    expect(creditLine(undefined, undefined)).toBe('AI 生成 · 模型服务');
  });
});
