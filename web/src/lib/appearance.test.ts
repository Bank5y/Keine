import { describe, expect, it } from 'vitest';
import { decodeFont, encodeFont, familyStack, KEYS, readAppearance } from '@/lib/appearance';
import { families, splitMonospace } from '@/lib/local-fonts';

describe('font choices', () => {
  it('stores nothing for the default, so a fresh browser and a reset one look the same', () => {
    expect(encodeFont({ preset: 'default' })).toBeNull();
    expect(encodeFont({ family: '   ' })).toBeNull();
  });

  it('round-trips a preset and a family', () => {
    expect(decodeFont('read', encodeFont({ preset: 'kai' }))).toEqual({ preset: 'kai' });
    expect(decodeFont('code', encodeFont({ family: ' Iosevka ' }))).toEqual({ family: 'Iosevka' });
  });

  it('reads an unknown or foreign preset as the default', () => {
    expect(decodeFont('read', 'nope')).toEqual({ preset: 'default' });
    expect(decodeFont('code', 'kai')).toEqual({ preset: 'default' });
    expect(decodeFont('read', 'family:')).toEqual({ preset: 'default' });
  });

  it('falls back to the default stack behind a family, and keeps quotes out', () => {
    expect(familyStack('read', 'LXGW WenKai')).toBe('"LXGW WenKai", var(--serif)');
    expect(familyStack('code', 'a"b\\c')).toBe('"abc", var(--mono)');
  });
});

describe('readAppearance', () => {
  it('starts from 石墨, following the system, with the default fonts', () => {
    expect(readAppearance(() => null)).toEqual({ palette: 'graphite', mode: 'system', read: { preset: 'default' }, code: { preset: 'default' } });
  });

  it('reads what the browser saved, including the masthead toggle\'s key', () => {
    const saved: Record<string, string> = { [KEYS.palette]: 'mokou', [KEYS.mode]: 'dark', [KEYS.read]: 'song', [KEYS.code]: 'family:Hack' };
    expect(readAppearance((k) => saved[k] ?? null)).toEqual({ palette: 'mokou', mode: 'dark', read: { preset: 'song' }, code: { family: 'Hack' } });
  });
});

describe('detected fonts', () => {
  it('dedupes families ignoring case and sorts them', () => {
    expect(families(['Menlo', 'Arial', 'menlo', ' ', 'Arial '])).toEqual(['Arial', 'Menlo']);
  });

  it('puts monospace fonts first for the code font', () => {
    expect(splitMonospace(['Arial', 'Hack', 'Menlo'], (f) => f !== 'Arial')).toEqual({ mono: ['Hack', 'Menlo'], other: ['Arial'] });
  });
});
