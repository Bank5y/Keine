import { useCallback, useRef, useState, type ReactNode } from 'react';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { formatBytes, type VideoSummary } from '@/lib/videos';

// useVideoNotice tells an uploader, before an upload starts, how many videos
// it carries and how much storage they take. It only informs: the uploader
// can go on or cancel. Render `notice` somewhere in the component and await
// `confirmVideos` before uploading:
//
//   const { confirmVideos, notice } = useVideoNotice();
//   if (!(await confirmVideos(await videosInZip(file)))) return;
//
// An upload without videos is confirmed at once, with no dialog.
export function useVideoNotice(): { confirmVideos: (videos: VideoSummary) => Promise<boolean>; notice: ReactNode } {
  const [videos, setVideos] = useState<VideoSummary | null>(null);
  const answer = useRef<((go: boolean) => void) | null>(null);

  const confirmVideos = useCallback((v: VideoSummary) => {
    if (!v.count) return Promise.resolve(true);
    answer.current?.(false);
    setVideos(v);
    return new Promise<boolean>((resolve) => { answer.current = resolve; });
  }, []);

  const settle = (go: boolean) => { answer.current?.(go); answer.current = null; setVideos(null); };

  const notice = (
    <Dialog open={!!videos} onOpenChange={(v) => { if (!v) settle(false); }} title="上传的内容含视频">
      {videos ? <p className="mt-2">内含 {videos.count} 个视频, 共 {formatBytes(videos.bytes)}, 会占用较多存储空间。确定继续上传吗?</p> : null}
      <div className="head-actions mt-5">
        <Button onClick={() => settle(false)}>取消</Button>
        <Button variant="default" onClick={() => settle(true)}>继续上传</Button>
      </div>
    </Dialog>
  );
  return { confirmVideos, notice };
}
