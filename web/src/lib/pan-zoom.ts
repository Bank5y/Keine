// The lightbox's view of a picture (#184): its scale, and where its top
// left corner is on the screen.
export interface View { scale: number; x: number; y: number }
export interface Size { w: number; h: number }

// a small diagram is enlarged to fit, but no more than this
export const MAX_FIT = 3;

// fitView shows all of a picture, centred in the box, pad kept free round it.
// A small picture is enlarged to fit, up to maxFit times: a bitmap passes 1,
// since past its own size it only blurs (#187).
export function fitView(picture: Size, box: Size, pad: number, maxFit = MAX_FIT): View {
  const scale = Math.min((box.w - 2 * pad) / picture.w, (box.h - 2 * pad) / picture.h, maxFit);
  return { scale, x: (box.w - picture.w * scale) / 2, y: (box.h - picture.h * scale) / 2 };
}

// zoomAt scales the view by factor about point p on the screen, so the
// part of the picture under p stays there, within the scale's limits.
export function zoomAt(v: View, factor: number, p: { x: number; y: number }, limits: { min: number; max: number }): View {
  const scale = Math.min(limits.max, Math.max(limits.min, v.scale * factor));
  const k = scale / v.scale;
  return { scale, x: p.x - (p.x - v.x) * k, y: p.y - (p.y - v.y) * k };
}
