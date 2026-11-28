// The site's embedding settings (#227): off, an OpenAI-compatible API, or
// a small model run on the server that the admin downloads.

export type EmbeddingProvider = 'off' | 'external' | 'local';

export interface LocalModel { id: string; name: string; dims: number; size: number }
export interface Download { model: string; status: 'running' | 'done' | 'failed'; done: number; total: number; error?: string }

export interface EmbeddingSettings {
  provider: EmbeddingProvider;
  baseUrl: string;
  model: string;
  keyLast4: string;
  rate: number;
  localModel: string;
  mirror: string;
  generation: number;
  dims: number;
  canStoreKeys: boolean;
  models: LocalModel[];
  downloads: Download[];
}

// EmbeddedCourse is how far a course's current version is embedded.
export interface EmbeddedCourse { courseId: number; title: string; done: number; total: number }

export interface EmbeddingState { settings: EmbeddingSettings; courses: EmbeddedCourse[] }

// megabytes shows a size in MB, one decimal.
export const megabytes = (bytes: number) => (bytes / 1e6).toFixed(1);

// downloadLabel says how far a download got: "42.1 / 90.5 MB".
export const downloadLabel = (d: Pick<Download, 'done' | 'total'>) => `${megabytes(d.done)} / ${megabytes(d.total)} MB`;

// embedState names how far a course is embedded, with the tone of its dot.
export function embedState(c: Pick<EmbeddedCourse, 'done' | 'total'>): { label: string; tone: '' | 'ok' | 'warn' } {
  if (c.total > 0 && c.done >= c.total) return { label: '已嵌入', tone: 'ok' };
  return c.done > 0 ? { label: '嵌入中', tone: 'warn' } : { label: '等待中', tone: '' };
}

// stillEmbedding tells whether the page has something to follow: a
// course being embedded, or a download under way.
export const stillEmbedding = (s: EmbeddingState) =>
  s.settings.downloads.some((d) => d.status === 'running') || (s.settings.provider !== 'off' && s.courses.some((c) => c.done < c.total));
