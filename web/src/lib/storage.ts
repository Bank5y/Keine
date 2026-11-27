// The object storage settings of the admin console: course media in an S3
// compatible bucket, how browsers get it, and moving files between the
// bucket and local disk.

export type Media = 'video' | 'media';
export type Delivery = 'proxy' | 'presign' | 'cdn';
export type Direction = 'push' | 'sync' | 'pull';

export interface StorageSettings {
  enabled: boolean; endpoint: string; region: string; bucket: string; accessKey: string; prefix: string;
  insecure: boolean; pathStyle: boolean; createBucket: boolean;
  media: Media; delivery: Delivery; presignSeconds: number; cdnBase: string;
}

export interface Migration {
  direction: Direction; state: 'running' | 'done' | 'failed' | 'cancelled'; started: string; finished?: string; error?: string;
  total: number; done: number; copied: number; skipped: number; failed: number; bytes: number; errors: string[] | null;
}

export interface StorageState extends StorageSettings {
  secretLast4?: string; secretSet: boolean; secretFromEnv: boolean; masterKey: boolean;
  status: { connected: boolean; active: boolean; error?: string };
  migration: Migration | null;
}

export interface ProbeResult { ok: boolean; steps: { name: string; ok: boolean; ms: number; error?: string }[] }

// The form keeps numbers as text while they are typed; presign in minutes.
export interface StorageForm extends Omit<StorageSettings, 'presignSeconds'> { presignMinutes: string; secretKey: string }

export const MEDIA: [Media, string, string][] = [
  ['video', '仅视频', '视频放到对象存储, 图片和文字留在本地'],
  ['media', '视频与图片', '视频和图片都放到对象存储, 文字留在本地'],
];

export const DELIVERY: [Delivery, string, string][] = [
  ['proxy', '服务器代理', '文件经 Keine 服务器转发, 桶可以完全私有'],
  ['presign', 'Presign', '跳转到带签名的临时地址, 浏览器直接从桶下载'],
  ['cdn', 'CDN', '跳转到 CDN 上的地址, CDN 需能读取桶'],
];

export const DIRECTIONS: [Direction, string, string][] = [
  ['push', '推送', '本地 → 云: 把媒体文件移到对象存储, 完成后删除本地副本'],
  ['sync', '同步', '本地 → 云: 把媒体文件复制到对象存储, 保留本地副本'],
  ['pull', '拉取', '云 → 本地: 把对象存储里的文件复制回本地, 云端不删'],
];

export const STEP_NAMES: Record<string, string> = { connect: '连接', write: '写入', read: '读取', delete: '删除' };

export function formOf(s: StorageSettings): StorageForm {
  const { presignSeconds, ...rest } = s;
  return { ...rest, presignMinutes: String(Math.round(presignSeconds / 60)), secretKey: '' };
}

// payload is the form as the API takes it; the server validates it.
export function payload(f: StorageForm) {
  const { presignMinutes, ...rest } = f;
  const minutes = Number(presignMinutes.trim());
  return {
    ...rest,
    endpoint: f.endpoint.trim(), bucket: f.bucket.trim(), prefix: f.prefix.trim(), cdnBase: f.cdnBase.trim(),
    presignSeconds: Number.isFinite(minutes) && minutes > 0 ? Math.round(minutes * 60) : 0,
  };
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

const STATE_NAMES: Record<Migration['state'], string> = { running: '进行中', done: '已完成', failed: '失败', cancelled: '已取消' };

// migrationLine sums a migration up in one line.
export function migrationLine(m: Migration): string {
  const name = DIRECTIONS.find(([d]) => d === m.direction)?.[1] ?? m.direction;
  const parts = [`${name}${STATE_NAMES[m.state]}`, `${m.done}/${m.total} 个文件`];
  if (m.copied) parts.push(`复制 ${m.copied} 个 (${formatBytes(m.bytes)})`);
  if (m.skipped) parts.push(`跳过 ${m.skipped} 个已有的`);
  if (m.failed) parts.push(`失败 ${m.failed} 个`);
  return parts.join(' · ');
}

export const progressOf = (m: Migration) => (m.total ? m.done / m.total : m.state === 'running' ? 0 : 1);
