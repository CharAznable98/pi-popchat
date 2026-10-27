import { useMemo } from "react";
import katex from "katex";
import "katex/dist/katex.min.css";

export function MathFormula({
  tex,
  source,
  display,
  closed,
}: {
  tex: string;
  source: string;
  display: boolean;
  closed: boolean;
}) {
  const html = useMemo(() => {
    if (!closed) return null;
    try {
      return katex.renderToString(tex, {
        displayMode: display,
        throwOnError: true,
        trust: false,
        strict: "ignore",
        maxExpand: 1000,
        maxSize: 20,
      });
    } catch {
      return null;
    }
  }, [tex, display, closed]);
  return html === null ? (
    <code className="math-source">{source}</code>
  ) : (
    <span
      className={display ? "math-formula display" : "math-formula"}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}
