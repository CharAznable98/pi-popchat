import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Message, UIAction } from "../types";
import { fileTarget } from "../state";
import { MessageImage, type ImageReader } from "./MessageImage";
import { CodeBlock } from "./AgentContent";
export function MessageView({
  message: m,
  cwd,
  sessionId,
  readImage,
  openLink,
  run,
}: {
  message: Message;
  cwd: string;
  sessionId: string;
  readImage: ImageReader;
  openLink: (href: string, reveal?: boolean) => void;
  run: UIAction;
}) {
  return (
    <article className={"message " + m.role}>
      {m.status === "uncertain" && (
        <div className="message-warning warning">
          交付状态不确定，请确认后再重试
        </div>
      )}
      {m.text && (
        <Markdown
          remarkPlugins={[remarkGfm]}
          urlTransform={(url) => url}
          components={{
            a: ({ href, children }) => (
              <span className="file-link">
                <a
                  href="#"
                  onClick={(e) => {
                    e.preventDefault();
                    openLink(href || "");
                  }}
                >
                  {children}
                </a>
                {href && fileTarget(href, cwd)?.action === "openFile" && (
                  <button
                    title="在 Finder 中显示"
                    aria-label="在 Finder 中显示"
                    onClick={() => openLink(href, true)}
                  >
                    ↗
                  </button>
                )}
              </span>
            ),
            img: ({ src, alt }) => (
              <MessageImage
                key={`${sessionId}:${src}`}
                src={src}
                alt={alt}
                cwd={cwd}
                sessionId={sessionId}
                readImage={readImage}
                openLink={openLink}
              />
            ),
            pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
          }}
        >
          {m.text}
        </Markdown>
      )}
      {m.attachments?.map((a) => (
        <button
          className="attachment sent"
          key={a.id}
          onClick={() => run("openFile", { path: a.path })}
        >
          {a.preview && <img src={a.preview} alt={a.name} loading="lazy" />}▧{" "}
          {a.name}
        </button>
      ))}
    </article>
  );
}
