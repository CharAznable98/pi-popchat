import { useRef, useState, type ReactNode } from "react";
import type { Interaction } from "../types";
export function CodeBlock({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLPreElement>(null),
    [copied, setCopied] = useState(false),
    [copyError, setCopyError] = useState(false);
  return (
    <div className="code-block">
      <button
        onClick={() => {
          void navigator.clipboard
            .writeText(ref.current?.textContent || "")
            .then(() => {
              setCopied(true);
              setCopyError(false);
              setTimeout(() => setCopied(false), 1500);
            })
            .catch(() => setCopyError(true));
        }}
      >
        {copyError ? "复制失败，请手动选择" : copied ? "已复制" : "复制代码"}
      </button>
      <pre ref={ref}>{children}</pre>
    </div>
  );
}
export function InteractionCard({
  interaction: i,
  submit: onSubmit,
}: {
  interaction: Interaction;
  submit: (value: unknown, cancelled?: boolean) => Promise<unknown>;
}) {
  const [value, setValue] = useState(i.defaultValue || ""),
    [pending, setPending] = useState(false);
  const lock = useRef(false);
  const submit = async (value: unknown, cancelled?: boolean) => {
    if (lock.current) return;
    lock.current = true;
    setPending(true);
    try {
      await onSubmit(value, cancelled);
    } catch {
    } finally {
      lock.current = false;
      setPending(false);
    }
  };
  return (
    <section className="interaction">
      <small>需要你的回答</small>
      <h3>{i.title || "请确认"}</h3>
      {i.message && <p>{i.message}</p>}
      {i.method === "confirm" ? (
        <div>
          <button
            disabled={pending}
            className="primary"
            onClick={() => submit(true)}
          >
            确认
          </button>
          <button disabled={pending} onClick={() => submit(false)}>
            拒绝
          </button>
        </div>
      ) : i.method === "select" ? (
        <div className="options">
          {i.options?.map((o) => (
            <button disabled={pending} key={o} onClick={() => submit(o)}>
              {o}
            </button>
          ))}
        </div>
      ) : (
        <>
          <textarea
            aria-label="回答 Agent"
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
          <button
            disabled={pending}
            className="primary"
            onClick={() => submit(value)}
          >
            提交回答
          </button>
        </>
      )}
      <button
        disabled={pending}
        className="subtle"
        onClick={() => submit(null, true)}
      >
        取消
      </button>
    </section>
  );
}
