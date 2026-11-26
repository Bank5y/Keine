import { describe, expect, it } from 'vitest';
import { formatBytes, formOf, migrationLine, payload, progressOf, type Migration, type StorageSettings } from './storage';

const settings: StorageSettings = {
  enabled: true, endpoint: 'http://s3:9000', region: 'us-east-1', bucket: 'keine', accessKey: 'k', prefix: '',
  insecure: false, pathStyle: true, createBucket: true, media: 'video', delivery: 'presign', presignSeconds: 3600, cdnBase: '',
};

describe('storage form', () => {
  it('shows the presign lifetime in minutes and sends it in seconds', () => {
    const f = formOf(settings);
    expect(f.presignMinutes).toBe('60');
    expect(f.secretKey).toBe('');
    expect(payload({ ...f, presignMinutes: '15' }).presignSeconds).toBe(900);
  });

  it('trims what was typed and leaves a blank lifetime to the server', () => {
    const p = payload({ ...formOf(settings), endpoint: ' http://s3:9000 ', bucket: ' keine ', presignMinutes: '' });
    expect(p.endpoint).toBe('http://s3:9000');
    expect(p.bucket).toBe('keine');
    expect(p.presignSeconds).toBe(0);
    expect('presignMinutes' in p).toBe(false);
  });
});

describe('migration summary', () => {
  const m: Migration = { direction: 'push', state: 'done', started: '', total: 5, done: 5, copied: 3, skipped: 1, failed: 1, bytes: 3 * 1024 * 1024, errors: [] };

  it('says what moved', () => {
    expect(migrationLine(m)).toBe('推送已完成 · 5/5 个文件 · 复制 3 个 (3.0 MB) · 跳过 1 个已有的 · 失败 1 个');
  });

  it('measures progress, full when there was nothing to move', () => {
    expect(progressOf({ ...m, done: 2 })).toBe(0.4);
    expect(progressOf({ ...m, total: 0, done: 0 })).toBe(1);
    expect(progressOf({ ...m, total: 0, done: 0, state: 'running' })).toBe(0);
  });

  it('formats sizes', () => {
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(20 * 1024 ** 3)).toBe('20 GB');
  });
});
