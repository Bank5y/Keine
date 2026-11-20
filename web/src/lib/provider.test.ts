import { describe, expect, it } from 'vitest';
import { chooseProvider, type Preset } from '@/lib/provider';

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
