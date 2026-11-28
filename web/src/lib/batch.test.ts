import { describe, expect, it } from 'vitest';
import { runInTurn } from '@/lib/batch';

describe('runInTurn', () => {
  it('runs one item at a time, in order, and goes on past a failure', async () => {
    const log: string[] = [];
    let running = 0;
    const results = await runInTurn(['a', 'b', 'c'], async (x) => {
      running++;
      expect(running).toBe(1);
      log.push(x);
      await Promise.resolve();
      running--;
      if (x === 'b') throw new Error('坏了');
      return x.toUpperCase();
    });
    expect(log).toEqual(['a', 'b', 'c']);
    expect(results).toEqual([
      { item: 'a', ok: true, value: 'A' },
      { item: 'b', ok: false, error: new Error('坏了') },
      { item: 'c', ok: true, value: 'C' },
    ]);
  });
});
