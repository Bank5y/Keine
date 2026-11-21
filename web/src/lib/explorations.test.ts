import { describe, expect, it } from 'vitest';
import { overlapping, type Exploration } from '@/lib/explorations';

const at = (id: number, start: number, end: number): Exploration => ({
  id, quote: 'q', prefix: '', suffix: '', state: 'attached', title: `探索 ${id}`, intro: '简介', range: { start, end }, createdAt: '',
});

describe('overlapping', () => {
  const list = [at(1, 10, 15), at(2, 30, 40)];

  it('finds the exploration a selection shares a character with', () => {
    expect(overlapping({ start: 12, end: 20 }, list)?.id).toBe(1);
    expect(overlapping({ start: 0, end: 11 }, list)?.id).toBe(1);
    expect(overlapping({ start: 32, end: 33 }, list)?.id).toBe(2);
    // a selection holding a whole exploration
    expect(overlapping({ start: 25, end: 45 }, list)?.id).toBe(2);
  });

  it('lets a selection that only touches one go', () => {
    expect(overlapping({ start: 15, end: 30 }, list)).toBeNull();
    expect(overlapping({ start: 0, end: 10 }, list)).toBeNull();
    expect(overlapping({ start: 40, end: 50 }, list)).toBeNull();
  });

  it('skips explorations not placed in the text', () => {
    expect(overlapping({ start: 10, end: 15 }, [{ ...at(3, 0, 0), range: null }])).toBeNull();
  });

  it('finds nothing for a selection it cannot place', () => {
    expect(overlapping(null, list)).toBeNull();
  });
});
