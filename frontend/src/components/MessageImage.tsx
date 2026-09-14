import { useEffect, useState } from "react";
import { fileTarget } from "../state";

export type ImageReader = (sessionId: string, path: string) => Promise<string>;
export function MessageImage({
  src,
  alt,
  cwd,
  sessionId,
  readImage,
  openLink,
}: {
  src?: string;
  alt?: string;
  cwd: string;
  sessionId: string;
  readImage: ImageReader;
  openLink: (href: string) => void;
}) {
  const [resolved, setResolved] = useState<string>();
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const source = src?.trim() || "";
  const inline =
    /^data:image\/(png|jpeg|gif|webp);base64,[a-z0-9+/=\s]+$/i.test(source);
  const target = source ? fileTarget(source, cwd) : null;
  const remote = target?.action === "openURL";
  const supported = inline || !!target;
  useEffect(() => {
    let active = true;
    setFailed(false);
    setResolved(undefined);
    if (!supported) {
      setFailed(true);
      return;
    }
    if (inline || remote) {
      setResolved(source);
      return;
    }
    readImage(sessionId, target!.payload.path).then(
      (data) => {
        if (active) setResolved(data);
      },
      () => {
        if (active) setFailed(true);
      },
    );
    return () => {
      active = false;
    };
  }, [source, cwd, sessionId, readImage, attempt]);
  return (
    <span className="message-image">
      {failed ? (
        <span className="image-fallback">
          <span>{alt || "图片"} · 图片加载失败</span>
          {supported && (
            <button type="button" onClick={() => setAttempt(attempt + 1)}>
              重试
            </button>
          )}
          {target && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                openLink(source);
              }}
            >
              打开原图
            </button>
          )}
        </span>
      ) : resolved ? (
        <button
          type="button"
          className="image-preview"
          title="打开原图"
          disabled={inline}
          onClick={(e) => {
            e.stopPropagation();
            openLink(source);
          }}
        >
          <img
            key={attempt}
            src={resolved}
            alt={alt || "图片"}
            loading="lazy"
            decoding="async"
            referrerPolicy="no-referrer"
            onError={() => setFailed(true)}
          />
        </button>
      ) : (
        <span className="muted" role="status">
          正在加载图片…
        </span>
      )}
    </span>
  );
}
