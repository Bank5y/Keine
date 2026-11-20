import { describe, expect, it } from 'vitest';
import { chooseProvider, keepRoute, searchLabel, searchRoutes, type Preset } from '@/lib/provider';

const deepseek: Preset = { id: 'deepseek', name: 'DeepSeek', format: 'openai', baseUrl: 'https://api.deepseek.com', displayName: 'DeepSeek' };
const presets = [deepseek];
const empty = { provider: '', baseUrl: '', displayName: '' };

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

describe('searchRoutes', () => {
  it('offers DeepSeek search only to a DeepSeek model', () => {
    expect(searchRoutes('deepseek').map((r) => r.id)).toEqual(['', 'deepseek', 'tavily', 'off']);
    expect(searchRoutes('').map((r) => r.id)).toEqual(['', 'tavily', 'off']);
  });

  it('says what leaving the route to resolve gives', () => {
    expect(searchRoutes('deepseek')[0].label).toBe('自动（DeepSeek 搜索和抓取）');
    expect(searchRoutes('')[0].label).toBe('自动（Tavily, 需要站点的 Tavily key）');
  });
});

describe('keepRoute', () => {
  it('drops a route the new provider does not offer', () => {
    expect(keepRoute('deepseek', '')).toBe('');
    expect(keepRoute('deepseek', 'deepseek')).toBe('deepseek');
    expect(keepRoute('off', '')).toBe('off');
  });
});

describe('searchLabel', () => {
  it('names what a model searches with now, or that it has no route', () => {
    expect(searchLabel({ search: 'deepseek', searchReady: true })).toBe('DeepSeek 搜索和抓取');
    expect(searchLabel({ search: 'tavily', searchReady: true })).toBe('Tavily 搜索');
    expect(searchLabel({ search: 'off', searchReady: true })).toBe('不联网');
    expect(searchLabel({ search: 'tavily', searchReady: false })).toBe('没有搜索路线');
  });
});
