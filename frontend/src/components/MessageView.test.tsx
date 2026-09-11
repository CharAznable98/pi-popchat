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
