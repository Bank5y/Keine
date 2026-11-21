import { describe, expect, it } from 'vitest';
import { chooseFormat, chooseProvider, keepRoute, searchLabel, searchRoutes, type Format, type Preset } from '@/lib/provider';

const deepseek: Preset = { id: 'deepseek', name: 'DeepSeek', format: 'openai', baseUrl: 'https://api.deepseek.com', displayName: 'DeepSeek' };
const presets = [deepseek];
const empty = { provider: '', baseUrl: '', displayName: '' };
const formats: Format[] = [{ id: 'openai', name: 'OpenAI 兼容', baseUrl: '' }, { id: 'anthropic', name: 'Anthropic Messages', baseUrl: 'https://api.anthropic.com' }];

describe('chooseProvider', () => {
  it('fills in the address and display name of the chosen provider', () => {
    expect(chooseProvider(empty, 'deepseek', presets)).toEqual({ provider: 'deepseek', baseUrl: 'https://api.deepseek.com', displayName: 'DeepSeek' });
  });

  it('keeps what the admin typed', () => {
    const typed = { provider: '', baseUrl: 'https://gateway.example/v1', displayName: '我的 DeepSeek' };
    expect(chooseProvider(typed, 'deepseek', presets)).toEqual({ ...typed, provider: 'deepseek' });
  });

  it('takes the preset back out when the provider is unset', () => {
    const chosen = chooseProvider(empty, 'deepseek', presets);
    expect(chooseProvider(chosen, '', presets)).toEqual(empty);
    const edited = { ...chosen, displayName: 'DeepSeek Flash' };
    expect(chooseProvider(edited, '', presets)).toEqual({ provider: '', baseUrl: '', displayName: 'DeepSeek Flash' });
  });
});

describe('chooseFormat', () => {
  const openai = { ...empty, format: 'openai' };

  it('switches the format, fills in its address and drops a provider of another', () => {
    const ds = { ...chooseProvider(openai, 'deepseek', presets), searchRoute: 'deepseek' as const };
    expect(chooseFormat(ds, 'anthropic', formats, presets)).toEqual({ format: 'anthropic', provider: '', baseUrl: 'https://api.anthropic.com', displayName: 'DeepSeek', searchRoute: '' });
  });

  it('keeps an address the admin typed, and takes the default back out', () => {
    const typed = { ...openai, baseUrl: 'https://gateway.example', searchRoute: 'off' as const };
    expect(chooseFormat(typed, 'anthropic', formats, presets)).toEqual({ ...typed, format: 'anthropic' });
    const chosen = { ...chooseFormat({ ...openai, searchRoute: '' as const }, 'anthropic', formats, presets), searchRoute: 'provider' as const };
    expect(chooseFormat(chosen, 'openai', formats, presets)).toEqual({ ...openai, searchRoute: '' });
  });
});

describe('searchRoutes', () => {
  it('offers DeepSeek search only to a DeepSeek model', () => {
    expect(searchRoutes('openai', 'deepseek').map((r) => r.id)).toEqual(['', 'deepseek', 'tavily', 'off']);
    expect(searchRoutes('openai', '').map((r) => r.id)).toEqual(['', 'tavily', 'off']);
  });

  it('offers the provider route only to an Anthropic model', () => {
    expect(searchRoutes('anthropic', '').map((r) => r.id)).toEqual(['', 'provider', 'tavily', 'off']);
  });

  it('says what leaving the route to resolve gives', () => {
    expect(searchRoutes('openai', 'deepseek')[0].label).toBe('自动（DeepSeek 搜索和抓取）');
    expect(searchRoutes('openai', '')[0].label).toBe('自动（Tavily, 需要站点的 Tavily key）');
    expect(searchRoutes('anthropic', '')[0].label).toBe('自动（服务商自带搜索和抓取）');
  });
});

describe('keepRoute', () => {
  it('drops a route the new format or provider does not offer', () => {
    expect(keepRoute('deepseek', 'openai', '')).toBe('');
    expect(keepRoute('deepseek', 'openai', 'deepseek')).toBe('deepseek');
    expect(keepRoute('off', 'openai', '')).toBe('off');
    expect(keepRoute('provider', 'openai', '')).toBe('');
    expect(keepRoute('provider', 'anthropic', '')).toBe('provider');
  });
});

describe('searchLabel', () => {
  it('names what a model searches with now, or that it has no route', () => {
    expect(searchLabel({ search: 'deepseek', searchReady: true })).toBe('DeepSeek 搜索和抓取');
    expect(searchLabel({ search: 'tavily', searchReady: true })).toBe('Tavily 搜索');
    expect(searchLabel({ search: 'off', searchReady: true })).toBe('不联网');
    expect(searchLabel({ search: 'provider', searchReady: true })).toBe('服务商自带搜索和抓取');
    expect(searchLabel({ search: 'tavily', searchReady: false })).toBe('没有搜索路线');
  });
});
