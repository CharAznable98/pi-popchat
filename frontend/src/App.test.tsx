// @vitest-environment jsdom
import { beforeEach, afterEach, describe, it, expect, vi } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
  act,
} from "@testing-library/react";
import type { Snapshot } from "./types";
const mock = vi.hoisted(() => ({
  snapshot: vi.fn(),
  action: vi.fn(),
  save: vi.fn(),
  listener: () => {},
}));
vi.mock("./api", () => ({
  view: "panel",
  api: {
    snapshot: mock.snapshot,
    action: mock.action,
    save: mock.save,
    subscribe: (fn: () => void) => {
      mock.listener = fn;
      return () => {};
    },
  },
}));
import { App } from "./App";
function state(version = 1): Snapshot {
  return {
    version,
    currentId: "a",
    current: {
      id: "a",
      title: "测试会话",
      pinned: false,
      cwd: "/tmp/test",
      createdAt: "",
      updatedAt: "",
      status: "idle",
      error: "",
      draft: "",
      messages: [],
      queue: [],
      interaction: null,
      models: [],
      model: "",
      commands: [],
    },
    sessions: [],
    settings: { shortcut: "Alt+Space", piPath: "" },
    environment: {
      piPath: "/bin/pi",
      version: "0.84.1",
      available: true,
      error: "",
    },
    error: "",
  };
}
beforeEach(() => {
  vi.resetAllMocks();
  Element.prototype.scrollIntoView = vi.fn();
  mock.snapshot.mockResolvedValue(state());
  mock.action.mockResolvedValue(state(2));
});
afterEach(() => cleanup());
async function setup() {
  render(<App />);
  await screen.findByText("测试会话");
  return screen.getByRole("textbox", { name: "消息" });
}
describe("桌面对话交互", () => {
  it("中文组合输入期间 Enter 与 Escape 不发送不隐藏", async () => {
    const input = await setup();
    fireEvent.change(input, { target: { value: "中文输入" } });
    fireEvent.compositionStart(input);
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 });
    fireEvent.keyDown(input, { key: "Escape" });
    expect(mock.action).not.toHaveBeenCalled();
    fireEvent.compositionEnd(input);
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() =>
      expect(mock.action).toHaveBeenCalledWith(
        "send",
        expect.objectContaining({
          text: "中文输入",
          clientMessageId: expect.any(String),
        }),
      ),
    );
  });
  it("双击发送只交付一次，失败后重试复用消息ID", async () => {
    const input = await setup();
    let reject!: (e: Error) => void;
    mock.action.mockImplementationOnce(
      () =>
        new Promise((_, r) => {
          reject = r;
        }),
    );
    fireEvent.change(input, { target: { value: "执行一次" } });
    const button = screen.getByRole("button", { name: /^发送/ });
    fireEvent.click(button);
    fireEvent.click(button);
    expect(mock.action).toHaveBeenCalledTimes(1);
    const id = mock.action.mock.calls[0][1].clientMessageId;
    await act(async () => reject(new Error("连接中断")));
    expect((input as HTMLTextAreaElement).value).toBe("执行一次");
    fireEvent.click(screen.getByRole("button", { name: /^发送/ }));
    await waitFor(() => expect(mock.action).toHaveBeenCalledTimes(2));
    expect(mock.action.mock.calls[1][1].clientMessageId).toBe(id);
  });
  it("事件更新不覆盖正在编辑的本地草稿，也不接受旧快照", async () => {
    const input = await setup();
    fireEvent.change(input, { target: { value: "我的本地草稿" } });
    const next = state(5);
    next.current!.draft = "另一个窗口";
    next.current!.title = "新标题";
    mock.snapshot.mockResolvedValue(next);
    await act(async () => mock.listener());
    expect((input as HTMLTextAreaElement).value).toBe("我的本地草稿");
    expect(screen.getByText("新标题")).toBeTruthy();
    mock.snapshot.mockResolvedValue(state(3));
    await act(async () => mock.listener());
    expect(screen.getByText("新标题")).toBeTruthy();
  });
  it("草稿CAS冲突保留输入并显示解决入口", async () => {
    const input = await setup();
    mock.action.mockRejectedValue(new Error("draft conflict"));
    fireEvent.change(input, { target: { value: "不能丢失" } });
    await waitFor(
      () => expect(screen.getByText(/另一窗口已更新草稿/)).toBeTruthy(),
      { timeout: 1500 },
    );
    expect((input as HTMLTextAreaElement).value).toBe("不能丢失");
    expect(mock.action).toHaveBeenCalledWith(
      "draft",
      expect.objectContaining({ expectedDraft: "", text: "不能丢失", id: "a" }),
    );
  });
  it("Slash菜单Esc关闭而不删除用户输入，再按Esc收起浮窗", async () => {
    const s = state();
    s.current!.commands = [
      { name: "skill", description: "工作技能", source: "skill" },
    ];
    mock.snapshot.mockResolvedValue(s);
    const input = await setup();
    fireEvent.change(input, { target: { value: "/sk" } });
    expect(screen.getByRole("listbox")).toBeTruthy();
    fireEvent.keyDown(input, { key: "Escape" });
    expect(screen.queryByRole("listbox")).toBeNull();
    expect((input as HTMLTextAreaElement).value).toBe("/sk");
    fireEvent.keyDown(input, { key: "Escape" });
    await waitFor(() => expect(mock.action).toHaveBeenCalledWith("hide", {}));
  });
  it("明确交互卡片回传请求ID，不把普通回复当成确认", async () => {
    const s = state();
    s.current!.interaction = {
      id: "request-7",
      method: "confirm",
      title: "允许继续？",
    };
    mock.snapshot.mockResolvedValue(s);
    await setup();
    fireEvent.click(screen.getByRole("button", { name: "确认" }));
    expect(mock.action).toHaveBeenCalledWith("respond", {
      sessionId: "a",
      requestId: "request-7",
      value: true,
      cancelled: undefined,
    });
  });
});

it("首次模型信息尚未返回时显示加载状态，不误报配置缺失", async () => {
  await setup();
  expect(screen.queryByText("尚未获取到可用模型")).toBeNull();
  expect(screen.getByText("正在获取可用模型…")).toBeTruthy();
  const ready = state(3);
  ready.current!.models = [{ id: "test", name: "Test", provider: "test" }];
  mock.snapshot.mockResolvedValue(ready);
  await act(async () => mock.listener());
  expect(screen.queryByText("正在获取可用模型…")).toBeNull();
  expect(screen.queryByText("尚未获取到可用模型")).toBeNull();
});
it("只有模型查询成功但返回空列表时才展示配置提示", async () => {
  const empty = state();
  empty.current!.modelsState = "ready";
  mock.snapshot.mockResolvedValue(empty);
  await setup();
  expect(screen.getByText("尚未获取到可用模型")).toBeTruthy();
  expect(screen.queryByText("正在获取可用模型…")).toBeNull();
});
it("模型查询失败显示可重试错误，不误报没有模型", async () => {
  const failed = state();
  failed.current!.modelsState = "error";
  failed.current!.modelsError = "查询超时";
  mock.snapshot.mockResolvedValue(failed);
  await setup();
  expect(screen.getByText("获取模型失败")).toBeTruthy();
  expect(screen.getByText("查询超时")).toBeTruthy();
  expect(screen.queryByText("尚未获取到可用模型")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(mock.action).toHaveBeenCalledWith("refreshAgent", {
      sessionId: "a",
    }),
  );
});
