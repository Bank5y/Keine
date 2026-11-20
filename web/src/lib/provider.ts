// A provider configuration (ADR 0008) as the admin page offers it: choosing
// one fills in its address and display name, so only the key and the model
// name are left to type.

export interface Preset { id: string; name: string; format: string; baseUrl: string; displayName: string }

interface Fields { provider: string; baseUrl: string; displayName: string }

// chooseProvider switches the form to another provider (or none): a field
// still blank, or still holding the old provider's default, takes the new
// one's; what the admin typed stays.
export function chooseProvider<F extends Fields>(f: F, next: string, presets: Preset[]): F {
  const before = presets.find((p) => p.id === f.provider);
  const after = presets.find((p) => p.id === next);
  const pick = (value: string, key: 'baseUrl' | 'displayName') => (value === '' || value === before?.[key] ? after?.[key] ?? '' : value);
  return { ...f, provider: next, baseUrl: pick(f.baseUrl, 'baseUrl'), displayName: pick(f.displayName, 'displayName') };
}

// How a model's runs reach the web (ADR 0008, #128): left to resolve (a
// DeepSeek model searches with DeepSeek, any other with the site's Tavily
// key), a search service named, or no web at all.
export type SearchRoute = '' | 'tavily' | 'deepseek' | 'off';

const routeNames: Record<Exclude<SearchRoute, ''>, string> = { deepseek: 'DeepSeek 搜索和抓取', tavily: 'Tavily 搜索', off: '不联网' };

// searchRoutes are the routes a model of the provider can take, first the
// one that resolves.
export function searchRoutes(provider: string): { id: SearchRoute; label: string }[] {
  const ds = provider === 'deepseek';
  return [
    { id: '', label: ds ? '自动（DeepSeek 搜索和抓取）' : '自动（Tavily, 需要站点的 Tavily key）' },
    ...(ds ? [{ id: 'deepseek' as const, label: routeNames.deepseek }] : []),
    { id: 'tavily', label: `${routeNames.tavily}（站点的 Tavily key）` },
    { id: 'off', label: routeNames.off },
  ];
}

// keepRoute is the route a form keeps when its provider changes.
export const keepRoute = (route: SearchRoute, provider: string): SearchRoute =>
  searchRoutes(provider).some((r) => r.id === route) ? route : '';

// searchLabel is what a model searches with now, as the server resolved it.
export const searchLabel = (m: { search: SearchRoute; searchReady: boolean }) =>
  !m.searchReady || m.search === '' ? '没有搜索路线' : routeNames[m.search];
