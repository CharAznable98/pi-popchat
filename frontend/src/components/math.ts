import type { Root } from "mdast";
import type { Construct, State, Tokenizer } from "micromark-util-types";
import type { Processor } from "unified";
import type {} from "micromark-extension-math";
import type {} from "remark-parse";
import { visit } from "unist-util-visit";
import { markdownLineEnding } from "micromark-util-character";

// Tokenize before CommonMark consumes backslash escapes. Using the Markdown
// tokenizer also keeps code spans, fenced code, and indented code untouched.
const tokenize: Tokenizer = function (effects, ok, nok) {
  let close = 0;
  const closing: Construct = {
    tokenize(effects, ok, nok) {
      return (code) => {
        effects.enter("mathTextSequence");
        effects.consume(code);
        return (code) => {
          if (code !== close) return nok(code);
          effects.consume(code);
          effects.exit("mathTextSequence");
          return ok;
        };
      };
    },
  };
  const finish: State = (code) => {
    effects.exit("mathText");
    return ok(code);
  };
  const between: State = (code) => {
    if (code === null) return finish(code);
    if (markdownLineEnding(code)) {
      effects.enter("lineEnding");
      effects.consume(code);
      effects.exit("lineEnding");
      return between;
    }
    if (code === 92) return effects.attempt(closing, finish, escaped)(code);
    effects.enter("mathTextData");
    return data(code);
  };
  const data: State = (code) => {
    if (code === null || code === 92 || markdownLineEnding(code)) {
      effects.exit("mathTextData");
      return between(code);
    }
    effects.consume(code);
    return data;
  };
  const escaped: State = (code) => {
    effects.enter("mathTextData");
    effects.consume(code);
    return (code) => {
      if (code === null || markdownLineEnding(code)) {
        effects.exit("mathTextData");
        return between(code);
      }
      effects.consume(code);
      return data;
    };
  };
  return (code) => {
    effects.enter("mathText");
    effects.enter("mathTextSequence");
    effects.consume(code);
    return (code) => {
      if (code !== 40 && code !== 91) return nok(code);
      close = code === 40 ? 41 : 93;
      effects.consume(code);
      effects.exit("mathTextSequence");
      return between;
    };
  };
};

export function remarkBackslashMath(this: Processor) {
  const data = this.data();
  const extensions =
    data.micromarkExtensions || (data.micromarkExtensions = []);
  extensions.push({ text: { 92: { name: "popchatMath", tokenize } } });
}

// Retain the exact source for unfinished streams and KaTeX parse failures.
// Only parser-produced math nodes use this renderer; ```math stays code.
export function remarkMathSource() {
  return (tree: Root, file: { value: unknown }) => {
    const input = String(file.value);
    visit(tree, (node) => {
      if (node.type !== "math" && node.type !== "inlineMath") return;
      const source = input.slice(
        node.position?.start.offset,
        node.position?.end.offset,
      );
      let closed: boolean;
      if (source.startsWith("\\(")) closed = source.endsWith("\\)");
      else if (source.startsWith("\\[")) closed = source.endsWith("\\]");
      else {
        const opening = source.match(/^\$+/)?.[0] || "";
        if (node.type === "math") {
          const lines = source.split(/\r\n|\r|\n/);
          const ending = lines.at(-1)?.match(/^\s*(?:>\s*)*(\${2,})\s*$/)?.[1];
          closed =
            lines.length > 1 && !!ending && ending.length >= opening.length;
        } else
          closed =
            source.length >= opening.length * 2 && source.endsWith(opening);
      }
      const display =
        node.type === "math" ||
        source.startsWith("\\[") ||
        source.startsWith("$$");
      node.data = {
        hName: "span",
        hProperties: {
          "data-math-tex": closed ? node.value : "",
          "data-math-source": source,
          "data-math-display": display,
          "data-math-closed": closed,
        },
        hChildren: [{ type: "text", value: node.value }],
      };
    });
  };
}
