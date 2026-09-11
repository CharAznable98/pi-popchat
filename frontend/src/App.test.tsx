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

it("共享草稿发送冲突保留输入，确认后按新版本重试并复用消息ID", async () => {
  const s = state();
  s.current!.draftOnly = true;
  s.current!.draftRevision = 0;
  mock.snapshot.mockResolvedValue(s);
  const input = await setup();
  fireEvent.change(input, { target: { value: "本窗口输入" } });
  const remote = state(2);
  remote.current!.draftOnly = true;
  remote.current!.draft = "另一窗口输入";
  remote.current!.draftRevision = 1;
  mock.snapshot.mockResolvedValue(remote);
  mock.action.mockRejectedValueOnce(
    new Error("另一窗口已更新或发送草稿，请核对当前输入后重试"),
  );
  fireEvent.click(screen.getByRole("button", { name: /^发送/ }));
  await screen.findByRole("button", { name: "保留当前输入" });
  await act(async () => mock.listener());
  expect((input as HTMLTextAreaElement).value).toBe("本窗口输入");
  const saved = state(3);
  saved.current!.draftOnly = true;
  saved.current!.draft = "本窗口输入";
  saved.current!.draftRevision = 2;
  mock.action.mockResolvedValueOnce(saved);
  fireEvent.click(screen.getByRole("button", { name: "保留当前输入" }));
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "保留当前输入" })).toBeNull(),
  );
  const sent = state(4);
  sent.current!.draftRevision = 3;
  mock.action.mockResolvedValueOnce(sent);
  fireEvent.click(screen.getByRole("button", { name: /^发送/ }));
  await waitFor(() =>
    expect(mock.action.mock.calls.filter((c) => c[0] === "send")).toHaveLength(
      2,
    ),
  );
  const sends = mock.action.mock.calls.filter((c) => c[0] === "send");
  expect(sends[1][1]).toEqual(
    expect.objectContaining({
      id: "a",
      text: "本窗口输入",
      expectedDraftRevision: 2,
      clientMessageId: sends[0][1].clientMessageId,
    }),
  );
});

it.each(["发送拒绝后刷新", "发送等待中收到通知"])(
  "已保存草稿在%s时保留文字和附件",
  async (timing) => {
    const file = {
      id: "original-file",
      name: "original.txt",
      path: "/tmp/original.txt",
      mime: "text/plain",
    };
    const saved = state();
    saved.current!.draft = "已经保存的本窗口输入";
    saved.current!.draftRevision = 1;
    saved.current!.draftAttachments = [file];
    mock.snapshot.mockResolvedValue(saved);
    const input = await setup();
    let reject!: (error: Error) => void;
    mock.action.mockImplementationOnce(
      () =>
        new Promise((_, r) => {
          reject = r;
        }),
    );
    fireEvent.click(screen.getByRole("button", { name: /^发送/ }));
    await waitFor(() =>
      expect(mock.action).toHaveBeenCalledWith(
        "send",
        expect.objectContaining({ expectedDraftRevision: 1 }),
      ),
    );
    const remote = state(2);
    remote.current!.title = "远端更新已应用";
    remote.current!.draft = "另一窗口输入";
    remote.current!.draftRevision = 2;
    mock.snapshot.mockResolvedValue(remote);
    if (timing === "发送等待中收到通知") await act(async () => mock.listener());
    await act(async () =>
      reject(new Error("另一窗口已更新或发送草稿，请核对当前输入后重试")),
    );
    await screen.findByText("远端更新已应用");
    expect((input as HTMLTextAreaElement).value).toBe("已经保存的本窗口输入");
    expect(
      screen.getByRole("button", { name: "移除 original.txt" }),
    ).toBeTruthy();
    const retained = state(3);
    retained.current!.draft = saved.current!.draft;
    retained.current!.draftRevision = 3;
    retained.current!.draftAttachments = [file];
    mock.action.mockResolvedValueOnce(retained);
    fireEvent.click(screen.getByRole("button", { name: "保留当前输入" }));
    await waitFor(() =>
      expect(mock.action).toHaveBeenCalledWith(
        "draft",
        expect.objectContaining({
          text: saved.current!.draft,
          attachments: [file],
          expectedDraftRevision: 2,
        }),
      ),
    );
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "保留当前输入" })).toBeNull(),
    );
    mock.action.mockResolvedValueOnce(state(4));
    fireEvent.click(screen.getByRole("button", { name: /^发送/ }));
    await waitFor(() =>
      expect(
        mock.action.mock.calls.filter((c) => c[0] === "send"),
      ).toHaveLength(2),
    );
    const sends = mock.action.mock.calls.filter((c) => c[0] === "send");
    expect(sends[1][1]).toEqual(
      expect.objectContaining({
        text: saved.current!.draft,
        attachments: [file],
        expectedDraftRevision: 3,
        clientMessageId: sends[0][1].clientMessageId,
      }),
    );
  },
);

it("已保存草稿发送中切换会话，成功后不再恢复已经发送的输入", async () => {
  const saved = state();
  saved.current!.draft = "待提交内容";
  mock.snapshot.mockResolvedValue(saved);
  const input = await setup();
  let resolve!: (s: Snapshot) => void;
  mock.action.mockImplementationOnce(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  fireEvent.click(screen.getByRole("button", { name: /^发送/ }));
  await waitFor(() =>
    expect(mock.action).toHaveBeenCalledWith("send", expect.anything()),
  );
  const other = state(2);
  other.currentId = "b";
  other.current!.id = "b";
  other.current!.draft = "B 的输入";
  mock.snapshot.mockResolvedValue(other);
  await act(async () => mock.listener());
  await act(async () => resolve({ ...other, version: 3 }));
  expect((input as HTMLTextAreaElement).value).toBe("B 的输入");
  const sent = state(4);
  sent.current!.draftRevision = 1;
  mock.snapshot.mockResolvedValue(sent);
  await act(async () => mock.listener());
  expect((input as HTMLTextAreaElement).value).toBe("");
});

it("发送响应丢失后切换会话，返回重试仍使用原消息 ID", async () => {
  const saved = state();
  saved.current!.draft = "可能已执行的请求";
  mock.snapshot.mockResolvedValue(saved);
  await setup();
  mock.action.mockRejectedValueOnce(new Error("桥接响应丢失"));
  fireEvent.click(screen.getByRole("button", { name: /^发送/ }));
  await screen.findByText("Error: 桥接响应丢失");
  const first = mock.action.mock.calls.find((c) => c[0] === "send")![1];
  const other = state(2);
  other.currentId = "b";
  other.current!.id = "b";
  mock.snapshot.mockResolvedValue(other);
  await act(async () => mock.listener());
  const submitted = state(3);
  submitted.current!.draftRevision = 1;
  mock.snapshot.mockResolvedValue(submitted);
  await act(async () => mock.listener());
  // Simulate the stale draft save rejection and explicit conflict resolution.
  mock.action.mockRejectedValueOnce(new Error("另一窗口已更新草稿"));
  fireEvent.change(screen.getByRole("textbox", { name: "消息" }), {
    target: { value: "可能已执行的请求" },
  });
  await screen.findByRole("button", { name: "保留当前输入" });
  const retained = state(4);
  retained.current!.draft = "可能已执行的请求";
  retained.current!.draftRevision = 2;
  mock.action.mockResolvedValueOnce(retained);
  fireEvent.click(screen.getByRole("button", { name: "保留当前输入" }));
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "保留当前输入" })).toBeNull(),
  );
  mock.action.mockResolvedValueOnce(state(5));
  fireEvent.click(screen.getByRole("button", { name: /^发送/ }));
  await waitFor(() =>
    expect(mock.action.mock.calls.filter((c) => c[0] === "send")).toHaveLength(
      2,
    ),
  );
  const retry = mock.action.mock.calls.filter((c) => c[0] === "send")[1][1];
  expect(retry.clientMessageId).toBe(first.clientMessageId);
  expect(retry.id).toBe("a");
});

it("shows missing selection permission and refreshes after authorization", async () => {
  const denied = state(20);
  denied.settings.selection = {
    enabled: true,
    buttons: [{ id: "explain", name: "解释", template: "{{text}}" }],
  };
  denied.selectionPermission = false;
  mock.snapshot.mockResolvedValue(denied);
  mock.action.mockResolvedValue(denied);
  await setup();
  fireEvent.click(await screen.findByRole("button", { name: "打开权限设置" }));
  await waitFor(() =>
    expect(mock.action).toHaveBeenCalledWith("selectionPermission", {}),
  );
  const allowed = { ...denied, selectionPermission: true };
  mock.snapshot.mockResolvedValue(allowed);
  await act(async () => {
    mock.listener();
  });
  await waitFor(() =>
    expect(screen.queryByText("划词工具条尚未生效")).toBeNull(),
  );
});
