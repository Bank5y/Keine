import { describe, expect, it } from 'vitest';
import { downloadLabel, embedState, stillEmbedding, type EmbeddingState } from '@/lib/embedding';

describe('downloadLabel', () => {
  it('shows how far a download got in MB', () => {
    expect(downloadLabel({ done: 42_100_000, total: 90_480_000 })).toBe('42.1 / 90.5 MB');
  });
});

describe('embedState', () => {
  it('names how far a course is embedded', () => {
    expect(embedState({ done: 412, total: 412 })).toEqual({ label: '已嵌入', tone: 'ok' });
    expect(embedState({ done: 118, total: 260 })).toEqual({ label: '嵌入中', tone: 'warn' });
    expect(embedState({ done: 0, total: 318 })).toEqual({ label: '等待中', tone: '' });
  });
});

describe('stillEmbedding', () => {
  const state = (provider: 'off' | 'external', done: number, downloading = false): EmbeddingState => ({
    settings: { provider, baseUrl: '', model: '', keyLast4: '', rate: 0, localModel: '', mirror: '', generation: 1, dims: 0, canStoreKeys: true, models: [],
      downloads: downloading ? [{ model: 'm', status: 'running', done: 1, total: 2 }] : [] },
    courses: [{ courseId: 1, title: 'c', done, total: 10 }],
  });
  it('follows courses being embedded and downloads', () => {
    expect(stillEmbedding(state('external', 3))).toBe(true);
    expect(stillEmbedding(state('external', 10))).toBe(false);
    expect(stillEmbedding(state('off', 3))).toBe(false);
    expect(stillEmbedding(state('off', 3, true))).toBe(true);
  });
});
