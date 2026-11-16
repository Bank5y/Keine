// Thin client for the Keine JSON API. Every failure becomes an ApiError that
// carries the server's machine-readable code and, for invalid input, the list
// of individual problems.

export interface Problem { path?: string; message: string }

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string, public problems: Problem[] = []) {
    super(message);
  }
}

// onUnauthorized is told when the session is gone (expired, signed out
// elsewhere, account disabled) so the app can return to the sign-in page.
let onUnauthorized = () => {};
export function setUnauthorizedHandler(f: () => void) { onUnauthorized = f; }

async function parse(res: Response) {
  const text = await res.text();
  let body: any = null;
  try { body = text ? JSON.parse(text) : null; } catch { /* not json */ }
  if (!res.ok) {
    const e = body?.error;
    if (res.status === 401 && e?.code === 'unauthenticated') onUnauthorized();
    throw new ApiError(res.status, e?.code || 'http_' + res.status, e?.message || `请求失败 (${res.status})`, e?.problems || []);
  }
  return body;
}

export async function api<T = any>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  return parse(res);
}

// sendFile sends one file as the raw request body (PUT by default),
// reporting its progress (0..1). Aborting the signal stops the upload; the
// promise then rejects with an ApiError coded "aborted".
export function sendFile<T = any>(path: string, file: Blob, onProgress: (p: number) => void, signal?: AbortSignal, method = 'PUT'): Promise<T> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) { reject(new ApiError(0, 'aborted', '已取消上传')); return; }
    const xhr = new XMLHttpRequest();
    xhr.open(method, path);
    xhr.withCredentials = true;
    xhr.setRequestHeader('Content-Type', 'application/octet-stream');
    xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(e.loaded / e.total); };
    xhr.onerror = () => reject(new ApiError(0, 'network', '网络错误, 上传中断'));
    xhr.onabort = () => reject(new ApiError(0, 'aborted', '已取消上传'));
    xhr.onload = () => {
      signal?.removeEventListener('abort', stop);
      parse(new Response(xhr.status === 204 ? null : xhr.responseText, { status: xhr.status })).then(resolve, reject);
    };
    const stop = () => xhr.abort();
    signal?.addEventListener('abort', stop, { once: true });
    xhr.send(file);
  });
}

// upload posts a file as multipart form data and reports the upload progress
// (0..1); fetch cannot observe request progress, XMLHttpRequest can.
export function upload<T = any>(path: string, field: string, file: File, onProgress: (p: number) => void, method = 'POST'): Promise<T> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(method, path);
    xhr.withCredentials = true;
    xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(e.loaded / e.total); };
    xhr.onerror = () => reject(new ApiError(0, 'network', '网络错误, 上传中断'));
    xhr.onload = () => {
      const res = new Response(xhr.responseText, { status: xhr.status });
      parse(res).then(resolve, reject);
    };
    const form = new FormData();
    form.append(field, file);
    xhr.send(form);
  });
}
