import { describe, expect, it } from 'vitest';
import { fitView, zoomAt } from '@/lib/pan-zoom';

describe('fitView', () => {
  it('fits a wide picture to the screen width, centred', () => {
    // 2000x500 into 1040x840 with 20 padding: 1000 wide, scale 0.5
    expect(fitView({ w: 2000, h: 500 }, { w: 1040, h: 840 }, 20)).toEqual({ scale: 0.5, x: 20, y: 295 });
  });

  it('fits a tall picture to the screen height', () => {
    expect(fitView({ w: 400, h: 1600 }, { w: 1040, h: 840 }, 20)).toEqual({ scale: 0.5, x: 420, y: 20 });
  });

  it('enlarges a small picture, but not past three times', () => {
    expect(fitView({ w: 100, h: 50 }, { w: 1040, h: 840 }, 20).scale).toBe(3);
  });

  it('keeps a small bitmap at its own size, centred, when capped at 1', () => {
    expect(fitView({ w: 100, h: 50 }, { w: 1040, h: 840 }, 20, 1)).toEqual({ scale: 1, x: 470, y: 395 });
  });

  it('still shrinks a large bitmap to fit when capped at 1', () => {
    expect(fitView({ w: 2000, h: 500 }, { w: 1040, h: 840 }, 20, 1).scale).toBe(0.5);
  });
});

describe('zoomAt', () => {
  const limits = { min: 0.25, max: 8 };

  it('keeps the point under the pointer where it is', () => {
    const v = zoomAt({ scale: 1, x: 100, y: 50 }, 2, { x: 300, y: 250 }, limits);
    // the picture point (200, 200) was at (300, 250) and still is
    expect(v).toEqual({ scale: 2, x: -100, y: -150 });
  });

  it('stops at the limits', () => {
    expect(zoomAt({ scale: 6, x: 0, y: 0 }, 2, { x: 0, y: 0 }, limits).scale).toBe(8);
    expect(zoomAt({ scale: 0.3, x: 0, y: 0 }, 0.5, { x: 0, y: 0 }, limits).scale).toBe(0.25);
  });
});
