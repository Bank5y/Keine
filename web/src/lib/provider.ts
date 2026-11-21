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

// An interface format as the admin page offers it, with its default
// address ('' when it has none).
export interface Format { id: string; name: string; baseUrl: string }

// chooseFormat switches the form to another interface format: an address
// still blank or still holding a default (of a format or a provider) takes
// the new format's, a provider of another format is dropped, and so is a
// search route the format does not offer.
export function chooseFormat<F extends Fields & { format: string; searchRoute: SearchRoute }>(f: F, next: string, formats: Format[], presets: Preset[]): F {
  const defaults = [...formats.map((x) => x.baseUrl), ...presets.map((p) => p.baseUrl)];
  const provider = presets.some((p) => p.id === f.provider && p.format === next) ? f.provider : '';
  const baseUrl = f.baseUrl === '' || defaults.includes(f.baseUrl) ? formats.find((x) => x.id === next)?.baseUrl ?? '' : f.baseUrl;
  return { ...f, format: next, provider, baseUrl, searchRoute: keepRoute(f.searchRoute, next, provider) };
}

// How a model's runs reach the web (ADR 0008, #128, #129): left to resolve
// (an Anthropic model searches with the provider's own tools, a DeepSeek
// model with DeepSeek, any other with the site's Tavily key), a route
// named, or no web at all.
export type SearchRoute = '' | 'provider' | 'tavily' | 'deepseek' | 'off';

const routeNames: Record<Exclude<SearchRoute, ''>, string> = {
  provider: '服务商自带搜索和抓取', deepseek: 'DeepSeek 搜索和抓取', tavily: 'Tavily 搜索', off: '不联网',
};

// searchRoutes are the routes a model of the format and provider can take,
// first the one that resolves.
export function searchRoutes(format: string, provider: string): { id: SearchRoute; label: string }[] {
  const own = format === 'anthropic';
  const ds = provider === 'deepseek';
  const auto = own ? routeNames.provider : ds ? routeNames.deepseek : 'Tavily, 需要站点的 Tavily key';
  return [
    { id: '', label: `自动（${auto}）` },
    ...(own ? [{ id: 'provider' as const, label: routeNames.provider }] : []),
    ...(ds ? [{ id: 'deepseek' as const, label: routeNames.deepseek }] : []),
    { id: 'tavily', label: `${routeNames.tavily}（站点的 Tavily key）` },
    { id: 'off', label: routeNames.off },
  ];
}

// keepRoute is the route a form keeps when its format or provider changes.
export const keepRoute = (route: SearchRoute, format: string, provider: string): SearchRoute =>
  searchRoutes(format, provider).some((r) => r.id === route) ? route : '';

// routeChoice is the route an admin chose for a model, by name.
export const routeChoice = (r: SearchRoute) => (r === '' ? '自动' : routeNames[r]);

// searchLabel is what a model searches with now, as the server resolved it.
export const searchLabel = (m: { search: SearchRoute; searchReady: boolean }) =>
  !m.searchReady || m.search === '' ? '没有搜索路线' : routeNames[m.search];
