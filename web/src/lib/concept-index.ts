// The concept index of a course version (#158), as the admin page shows
// it: built only when an admin asks, charged to the site.

export type IndexStatus = 'none' | 'building' | 'built' | 'failed';

// IndexedCourse is a library course with its current version's index.
export interface IndexedCourse {
  id: number;
  title: string;
  updatedAt: string;
  lessons: number;
  status: IndexStatus;
  entries: number;
  error?: string;
  startedAt: string | null;
  finishedAt: string | null;
}

const STATES: Record<IndexStatus, { label: string; tone: '' | 'ok' | 'warn' | 'off' }> = {
  none: { label: '未建立', tone: '' },
  building: { label: '建立中', tone: 'warn' },
  built: { label: '已建立', tone: 'ok' },
  failed: { label: '失败', tone: 'off' },
};

// indexState is a status's name and the tone of its dot.
export const indexState = (s: IndexStatus) => STATES[s];

// indexAction is what the admin can do about a version's index: build it,
// or try again after it failed.
export function indexAction(s: IndexStatus): string | null {
  if (s === 'none') return '建立索引';
  if (s === 'failed') return '重试';
  return null;
}

// stillBuilding tells whether some index is being built, for the page to
// keep following it.
export const stillBuilding = (courses: { status: IndexStatus }[]) => courses.some((c) => c.status === 'building');

const ERRORS: Record<string, string> = {
  malformed_output: '模型的输出格式不对',
  output_truncated: '模型的输出被截断',
  timeout: '模型超时',
  rate_limited: '模型限流',
  interrupted: '服务重启, 建立被中断',
};

// indexError says why a build failed, by its run error code.
export const indexError = (code: string) => ERRORS[code] ?? '模型调用失败';
