import { useEffect, useRef } from 'react';
import { ExternalLink } from 'lucide-react';
import type { LessonVideo } from '@/lib/courses';

const SAVE_EVERY_S = 5;
const DONE_AT = 0.9;

// LessonVideoView plays a lesson's video. Our own player resumes where the
// viewer stopped, saves the position and finishes the lesson at 90%; an
// embedded platform player or a plain link cannot report progress, so those
// lessons are marked done by hand.
export function LessonVideoView({ video, position, onPosition, onNearlyDone }: {
  video: LessonVideo;
  position: number;
  onPosition: (seconds: number) => void;
  onNearlyDone: () => void;
}) {
  if (video.mode === 'embed') {
    return (
      <div className="video-frame">
        <iframe src={video.src} title="视频" allow="fullscreen; picture-in-picture; encrypted-media" allowFullScreen
          sandbox="allow-scripts allow-same-origin allow-presentation allow-popups" referrerPolicy="no-referrer" />
      </div>
    );
  }
  if (video.mode === 'link') {
    return (
      <a className="video-link" href={video.src} target="_blank" rel="noreferrer">
        <ExternalLink />
        <span><b>在新窗口打开视频</b><span className="u">{video.src}</span></span>
      </a>
    );
  }
  return <Player src={video.src} position={position} onPosition={onPosition} onNearlyDone={onNearlyDone} />;
}

function Player({ src, position, onPosition, onNearlyDone }: { src: string; position: number; onPosition: (s: number) => void; onNearlyDone: () => void }) {
  const ref = useRef<HTMLVideoElement>(null);
  // callbacks change every render; the effects below only need the latest
  const cb = useRef({ onPosition, onNearlyDone });
  cb.current = { onPosition, onNearlyDone };

  // attach the source; HLS needs hls.js outside Safari
  useEffect(() => {
    const v = ref.current;
    if (!v) return undefined;
    let destroy = () => {};
    const isHLS = /\.m3u8($|\?)/i.test(src);
    if (isHLS && !v.canPlayType('application/vnd.apple.mpegurl')) {
      let alive = true;
      void import('hls.js').then(({ default: Hls }) => {
        if (!alive || !Hls.isSupported()) return;
        const hls = new Hls();
        hls.loadSource(src);
        hls.attachMedia(v);
        destroy = () => hls.destroy();
      });
      return () => { alive = false; destroy(); };
    }
    v.src = src;
    return () => { v.removeAttribute('src'); v.load(); };
  }, [src]);

  useEffect(() => {
    const v = ref.current;
    if (!v) return undefined;
    let lastSaved = -1;
    let finished = false;
    const save = () => {
      const t = Math.floor(v.currentTime);
      if (t !== lastSaved && t > 0) { lastSaved = t; cb.current.onPosition(t); }
    };
    const onMeta = () => { if (position > 0 && position < v.duration - 5) v.currentTime = position; };
    const onTime = () => {
      if (Math.abs(v.currentTime - lastSaved) >= SAVE_EVERY_S) save();
      if (!finished && v.duration && v.currentTime / v.duration >= DONE_AT) { finished = true; cb.current.onNearlyDone(); }
    };
    v.addEventListener('loadedmetadata', onMeta);
    v.addEventListener('timeupdate', onTime);
    v.addEventListener('pause', save);
    return () => {
      save();
      v.removeEventListener('loadedmetadata', onMeta);
      v.removeEventListener('timeupdate', onTime);
      v.removeEventListener('pause', save);
    };
  }, [src]); // eslint-disable-line react-hooks/exhaustive-deps

  return <video ref={ref} className="video-player" controls preload="metadata" playsInline />;
}
