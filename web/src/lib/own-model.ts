// A reader's own model (ADR 0010) as the account page edits it. The reader
// picks one of three kinds: an OpenAI-compatible endpoint, an Anthropic
// one, or DeepSeek, which is the OpenAI format with its provider
// configuration and only needs a key and a model name.

export type OwnKind = 'openai' | 'anthropic' | 'deepseek';
export type OwnSearch = 'off' | 'provider' | 'tavily' | 'deepseek';

// OwnModel is the saved model as the server shows it: keys by their last
// four only.
export interface OwnModel {
  format: string; provider: string; baseUrl: string; model: string; contextWindow: number;
  search: OwnSearch; keyLast4: string; tavilyLast4: string; inUse: boolean;
}

// what /api/me says of own models
export interface OwnModelsState { enabled: boolean; inUse: boolean; offline: boolean }

export interface OwnForm {
  kind: OwnKind; baseUrl: string; key: string; model: string;
  // as typed; '' leaves the server's conservative default
  contextWindow: string;
  search: OwnSearch; tavilyKey: string;
}

export const DEEPSEEK_URL = 'https://api.deepseek.com';

export const emptyOwn: OwnForm = { kind: 'openai', baseUrl: '', key: '', model: '', contextWindow: '', search: 'off', tavilyKey: '' };

export const KINDS: { id: OwnKind; label: string }[] = [
  { id: 'openai', label: 'OpenAI 兼容' }, { id: 'anthropic', label: 'Anthropic' }, { id: 'deepseek', label: 'DeepSeek' },
];

// searchChoices are the ways a model of the kind can reach the web, no web
// first (the default).
export function searchChoices(kind: OwnKind): { id: OwnSearch; label: string }[] {
  return [
    { id: 'off', label: '不联网' },
    ...(kind === 'anthropic' ? [{ id: 'provider' as const, label: '服务商自带搜索（需要你的服务支持）' }] : []),
    ...(kind === 'deepseek' ? [{ id: 'deepseek' as const, label: 'DeepSeek 搜索（用同一个 key）' }] : []),
    { id: 'tavily', label: 'Tavily（填你自己的 key）' },
  ];
}

// withKind switches the form to another kind: an address still blank or
// still DeepSeek's takes the new kind's, and a search it does not offer
// goes back to none.
export function withKind(f: OwnForm, kind: OwnKind): OwnForm {
  const baseUrl = f.baseUrl === '' || f.baseUrl === DEEPSEEK_URL ? (kind === 'deepseek' ? DEEPSEEK_URL : '') : f.baseUrl;
  const search = searchChoices(kind).some((c) => c.id === f.search) ? f.search : 'off';
  return { ...f, kind, baseUrl, search };
}

// ownBody is what saving sends; an empty key keeps the stored one.
export function ownBody(f: OwnForm) {
  const n = Number.parseInt(f.contextWindow.trim(), 10);
  return {
    format: f.kind === 'anthropic' ? 'anthropic' : 'openai',
    provider: f.kind === 'deepseek' ? 'deepseek' : '',
    baseUrl: f.baseUrl.trim(),
    key: f.key.trim(),
    model: f.model.trim(),
    contextWindow: Number.isFinite(n) ? n : null,
    search: f.search,
    tavilyKey: f.search === 'tavily' ? f.tavilyKey.trim() : '',
  };
}

// ownForm is a saved model back in the form, its keys blank to keep them.
export function ownForm(m: OwnModel): OwnForm {
  const kind: OwnKind = m.provider === 'deepseek' ? 'deepseek' : m.format === 'anthropic' ? 'anthropic' : 'openai';
  return { kind, baseUrl: m.baseUrl, key: '', model: m.model, contextWindow: String(m.contextWindow), search: m.search, tavilyKey: '' };
}

// creditLine names the service that writes the answer, and says so when
// the reader's own model reaches no web.
export function creditLine(provider: string | undefined, own: OwnModelsState | undefined): string {
  return `AI 生成 · ${provider || '模型服务'}${own?.inUse && own.offline ? ' · 未联网' : ''}`;
}
