// Finds the video files in an upload before it starts, so the uploader can
// be told how much storage they will take. Only extensions are looked at;
// nothing is decoded.

// The extensions the reader plays itself (internal/course/video.go).
const VIDEO_EXTENSIONS = new Set(['.mp4', '.m4v', '.webm', '.ogv', '.mov', '.m3u8']);

export interface VideoSummary { count: number; bytes: number }

export const NO_VIDEOS: VideoSummary = { count: 0, bytes: 0 };

export function isVideoName(name: string): boolean {
  const base = name.slice(name.lastIndexOf('/') + 1);
  const dot = base.lastIndexOf('.');
  return dot > 0 && VIDEO_EXTENSIONS.has(base.slice(dot).toLowerCase());
}

function summarize(entries: Iterable<{ name: string; size: number }>): VideoSummary {
  let count = 0, bytes = 0;
  for (const e of entries) if (isVideoName(e.name)) { count++; bytes += e.size; }
  return { count, bytes };
}

// videosInFiles sums the videos among files picked one by one.
export function videosInFiles(files: Iterable<File>): VideoSummary {
  return summarize(Array.from(files, (f) => ({ name: f.name, size: f.size })));
}

// videosInZip sums the videos in a zip by reading only its central
// directory, so a zip of any size costs a few small reads. Sizes are the
// uncompressed ones: what the server will store. A file that is not a
// readable zip has no videos as far as we know; the server reports why it
// cannot import it.
export async function videosInZip(zip: Blob): Promise<VideoSummary> {
  try {
    return summarize(await zipEntries(zip));
  } catch {
    return NO_VIDEOS;
  }
}

const EOCD = 0x06054b50, ZIP64_LOCATOR = 0x07064b50, ZIP64_EOCD = 0x06064b50, CENTRAL = 0x02014b50;
const MAX16 = 0xffff, MAX32 = 0xffffffff;

async function read(blob: Blob, start: number, end: number): Promise<DataView> {
  return new DataView(await blob.slice(start, end).arrayBuffer());
}

async function zipEntries(zip: Blob): Promise<{ name: string; size: number }[]> {
  // the end-of-central-directory record sits in the last 22 bytes plus at
  // most a 64 KiB comment
  const tailStart = Math.max(0, zip.size - 22 - MAX16);
  const tail = await read(zip, tailStart, zip.size);
  let at = -1;
  for (let i = tail.byteLength - 22; i >= 0; i--) if (tail.getUint32(i, true) === EOCD) { at = i; break; }
  if (at < 0) throw new Error('not a zip');
  let count = tail.getUint16(at + 10, true);
  let dirSize = tail.getUint32(at + 12, true);
  let dirOffset = tail.getUint32(at + 16, true);

  // zip64: the locator just before the record points at the zip64 record
  if (at >= 20 && tail.getUint32(at - 20, true) === ZIP64_LOCATOR) {
    const recAt = Number(tail.getBigUint64(at - 20 + 8, true));
    const rec = await read(zip, recAt, recAt + 56);
    if (rec.getUint32(0, true) !== ZIP64_EOCD) throw new Error('bad zip64 record');
    count = Number(rec.getBigUint64(32, true));
    dirSize = Number(rec.getBigUint64(40, true));
    dirOffset = Number(rec.getBigUint64(48, true));
  }

  const dir = await read(zip, dirOffset, dirOffset + dirSize);
  const names = new TextDecoder();
  const out: { name: string; size: number }[] = [];
  let p = 0;
  for (let i = 0; i < count && p + 46 <= dir.byteLength; i++) {
    if (dir.getUint32(p, true) !== CENTRAL) throw new Error('bad central directory');
    let size = dir.getUint32(p + 24, true);
    const nameLen = dir.getUint16(p + 28, true), extraLen = dir.getUint16(p + 30, true), commentLen = dir.getUint16(p + 32, true);
    const name = names.decode(new Uint8Array(dir.buffer, dir.byteOffset + p + 46, nameLen));
    if (size === MAX32) {
      // the real size is the first field of the zip64 extra block
      for (let e = p + 46 + nameLen, end = e + extraLen; e + 4 <= end;) {
        const id = dir.getUint16(e, true), len = dir.getUint16(e + 2, true);
        if (id === 0x0001 && len >= 8) { size = Number(dir.getBigUint64(e + 4, true)); break; }
        e += 4 + len;
      }
    }
    if (!name.endsWith('/')) out.push({ name, size });
    p += 46 + nameLen + extraLen + commentLen;
  }
  return out;
}

// formatBytes writes a size the way the video notice shows it.
export function formatBytes(n: number): string {
  if (n >= 1024 ** 3) return `${(n / 1024 ** 3).toFixed(1)} GB`;
  if (n >= 1024 ** 2) return `${(n / 1024 ** 2).toFixed(1)} MB`;
  return `${Math.max(1, Math.round(n / 1024))} KB`;
}
