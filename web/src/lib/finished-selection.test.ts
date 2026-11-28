import { describe, expect, it } from 'vitest';
import { selectionTiming, type SelectionEvent } from '@/lib/finished-selection';

const run = (...events: SelectionEvent[]) => {
  const t = selectionTiming();
  return events.map((e) => t.on(e));
};

describe('selectionTiming', () => {
  it('waits out a drag and reads the selection once the pointer lifts', () => {
    expect(run({ type: 'down', onBar: false }, { type: 'change', barTapped: false }, { type: 'change', barTapped: false }, { type: 'up' }))
      .toEqual([null, 'hold', 'hold', 'read']);
  });

  it("reads a phone's long-press selection when the browser takes the press over", () => {
    // the cancel arrives as the same "up": no lift follows it
    expect(run({ type: 'down', onBar: false }, { type: 'change', barTapped: false }, { type: 'up' })).toEqual([null, 'hold', 'read']);
  });

  it('lets a selection made with keys settle before reading it', () => {
    expect(run({ type: 'change', barTapped: false }, { type: 'change', barTapped: false })).toEqual(['settle', 'settle']);
  });

  it('keeps the bar through a tap on it, which may clear the selection first', () => {
    expect(run({ type: 'down', onBar: true }, { type: 'change', barTapped: true }, { type: 'up' })).toEqual([null, 'hold', null]);
  });

  it('reads nothing on a lift that no press of its own began', () => {
    expect(run({ type: 'up' })).toEqual([null]);
  });
});
