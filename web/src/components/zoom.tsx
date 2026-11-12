import { Dialog as D } from 'radix-ui';

// ImageZoom shows one image over the page; click anywhere or press Esc to close.
export function ImageZoom({ src, onClose }: { src: string | null; onClose: () => void }) {
  return (
    <D.Root open={!!src} onOpenChange={(v) => { if (!v) onClose(); }}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-40 bg-black/75 data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <D.Content aria-describedby={undefined} className="fixed inset-0 z-50 flex cursor-zoom-out items-center justify-center p-6 outline-none" onClick={onClose}>
          <D.Title className="sr-only">查看图片</D.Title>
          {src ? <img src={src} alt="" className="max-h-full max-w-full rounded-[2px] bg-white object-contain shadow-2xl" /> : null}
        </D.Content>
      </D.Portal>
    </D.Root>
  );
}
