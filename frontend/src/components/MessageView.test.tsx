// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MessageView } from "./MessageView";
afterEach(cleanup);
it.each([
  [String.raw`参数 \(\theta_i\) 与损失 \(L(\theta)\)。`, 2, 0],
  [String.raw`参数 $\theta_i$。`, 1, 0],
  [
    String.raw`常规训练：\[\theta \leftarrow \theta-\eta\nabla L(\theta)\]`,
    1,
    1,
  ],
  ["$$\n\\frac{a+b}{2\\epsilon}\n$$", 1, 1],
  ["\\[\n\\frac{x}{y}\n\\]", 1, 1],
  ["> $$\n> x^2\n> $$", 1, 1],
  [String.raw`$$\begin{pmatrix}a&b\\c&d\end{pmatrix}$$`, 1, 1],
  [String.raw`\[\begin{aligned}a&=b\\c&=d\end{aligned}\]`, 1, 1],
])("renders delimited math: %s", (text, count, displayCount) => {
  const { container } = render(
    <MessageView {...defaults} message={message(text)} />,
  );
  expect(container.querySelectorAll(".katex")).toHaveLength(count);
  expect(container.querySelectorAll(".katex-display")).toHaveLength(
    displayCount,
  );
});
it.each([
  String.raw`\(\theta`,
  String.raw`\[\frac{x}{y}`,
  "$\\theta",
  "$$\n\\theta",
  String.raw`\(\unknowncommand{x}\)`,
  String.raw`$\frac{x}$`,
])("preserves unfinished or invalid formula source: %s", (text) => {
  const { container } = render(
    <MessageView {...defaults} message={message(text)} />,
  );
  expect(container.querySelector(".katex")).toBeNull();
  expect(container.textContent).toContain(text);
});
it("keeps code, including a math-labelled fence, unchanged", () => {
  const code = String.raw`\(\theta\) $x$ \[y\] $$z$$`;
  const text = "`" + code + "`\n\n```math\n" + code + "\n```\n\n    " + code;
  const { container } = render(
    <MessageView {...defaults} message={message(text)} />,
  );
  expect(container.querySelector(".katex")).toBeNull();
  expect(
    Array.from(container.querySelectorAll("code"), (n) =>
      n.textContent?.trim(),
    ),
  ).toEqual([code, code, code]);
});
it("finishes a streamed formula without changing saved source or enabling commands", () => {
  const partial = message(String.raw`\(\theta`);
  const { container, rerender } = render(
    <MessageView {...defaults} message={partial} />,
  );
  expect(container.textContent).toContain(partial.text);
  const complete = message(String.raw`\(\theta_i\)`);
  rerender(<MessageView {...defaults} message={complete} />);
  expect(container.querySelector(".katex")).toBeTruthy();
  expect(complete.text).toBe(String.raw`\(\theta_i\)`);
  rerender(
    <MessageView
      {...defaults}
      message={message(String.raw`$\href{javascript:alert(1)}{x}$`)}
    />,
  );
  expect(container.querySelector('[href^="javascript:"]')).toBeNull();
});
const imageData = "data:image/png;base64,iVBORw0KGgo=";
const message = (text: string) => ({
  id: "m",
  role: "assistant",
  text,
  status: "complete",
  createdAt: "",
  attachments: [],
});
const defaults = {
  sessionId: "original",
  cwd: "/synthetic",
  readImage: vi.fn().mockResolvedValue(imageData),
  openLink: vi.fn(),
  run: vi.fn(),
};
it("renders Markdown images, supports opening originals, and recovers from load failure", async () => {
  const openLink = vi.fn();
  render(
    <MessageView
      {...defaults}
      openLink={openLink}
      message={message("![测试图](https://example.test/synthetic.png)")}
    />,
  );
  const img = await screen.findByRole("img", { name: "测试图" });
  expect(img.getAttribute("src")).toBe("https://example.test/synthetic.png");
  expect(img.getAttribute("referrerpolicy")).toBe("no-referrer");
  fireEvent.click(img);
  expect(openLink).toHaveBeenCalledWith("https://example.test/synthetic.png");
  fireEvent.error(img);
  expect(screen.getByText("测试图 · 图片加载失败")).toBeTruthy();
  fireEvent.click(screen.getByText("重试"));
  expect(await screen.findByRole("img")).toBeTruthy();
});
it("resolves local image paths through the initiating session", async () => {
  const readImage = vi.fn().mockResolvedValue(imageData);
  render(
    <MessageView
      {...defaults}
      readImage={readImage}
      message={message("![本地图](./outputs/a.png)")}
    />,
  );
  await waitFor(() =>
    expect(readImage).toHaveBeenCalledWith(
      "original",
      "/synthetic/./outputs/a.png",
    ),
  );
  expect((await screen.findByRole("img")).getAttribute("src")).toBe(imageData);
});
it("blocks executable image schemes without reading files or offering to open them", async () => {
  const readImage = vi.fn();
  render(
    <MessageView
      {...defaults}
      readImage={readImage}
      message={message("![坏图](javascript:alert)")}
    />,
  );
  expect(await screen.findByText("坏图 · 图片加载失败")).toBeTruthy();
  expect(screen.queryByRole("img")).toBeNull();
  expect(screen.queryByText("打开原图")).toBeNull();
  expect(readImage).not.toHaveBeenCalled();
});
it("ignores delayed image reads after changing sessions", async () => {
  let finish!: (s: string) => void;
  const readImage = vi
    .fn()
    .mockReturnValueOnce(
      new Promise<string>((r) => {
        finish = r;
      }),
    )
    .mockResolvedValue(imageData);
  const { rerender } = render(
    <MessageView
      {...defaults}
      readImage={readImage}
      message={message("![旧图](a.png)")}
    />,
  );
  rerender(
    <MessageView
      {...defaults}
      sessionId="new"
      readImage={readImage}
      message={message("![新图](b.png)")}
    />,
  );
  await screen.findByRole("img", { name: "新图" });
  finish("data:image/png;base64,AAAA");
  await waitFor(() =>
    expect(screen.getByRole("img").getAttribute("src")).toBe(imageData),
  );
});
