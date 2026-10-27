// @vitest-environment jsdom
import { afterEach, beforeEach, it, expect, vi } from "vitest";
import {
  render,
  act,
  cleanup,
  waitFor,
  screen,
  fireEvent,
} from "@testing-library/react";
import type { Snapshot } from "./types";
const mock = vi.hoisted(() => ({
  snapshot: vi.fn(),
  action: vi.fn(),
  listener: () => {},
}));
vi.mock("./api", () => ({
  view: "panel",
  api: {
    snapshot: mock.snapshot,
    action: mock.action,
    readImage: vi.fn(),
    save: vi.fn(),
    subscribe: (fn: () => void) => {
      mock.listener = fn;
      return () => {};
    },
  },
}));
import { App } from "./App";
function state(version = 1, revision = 0, pending = false): Snapshot {
  return {
    version,
    panelRevision: revision,
    panelPending: pending,
    currentId: "synthetic",
    current: {
      id: "synthetic",
      title: "synthetic-" + version,
      status: "idle",
      draft: "",
      draftAttachments: [],
      messages: [],
      queue: [],
      models: [],
      commands: [],
      pinned: false,
      cwd: "/synthetic",
      createdAt: "",
      updatedAt: "",
      error: "",
      interaction: null,
      model: "",
    },
    sessions: [],
    settings: { shortcut: "Alt+Space", piPath: "" },
    environment: { available: true, piPath: "", version: "", error: "" },
    error: "",
  };
}
beforeEach(() => {
  vi.resetAllMocks();
  Element.prototype.scrollIntoView = vi.fn();
  mock.snapshot.mockResolvedValue(state());
  mock.action.mockResolvedValue(state(10));
});
afterEach(cleanup);
it("protects the first panel load until its initial session snapshot arrives", async () => {
  let finish!: (s: Snapshot) => void;
  mock.snapshot.mockReturnValue(
    new Promise<Snapshot>((resolve) => {
      finish = resolve;
    }),
  );
  render(<App />);
  const input = screen.getByRole("textbox", {
    name: "消息",
  }) as HTMLTextAreaElement;
  expect(input.disabled).toBe(true);
  await act(async () => finish(state()));
  await waitFor(() => expect(input.disabled).toBe(false));
});
async function setup() {
  const subject = render(<App />);
  await screen.findByText("synthetic-1");
  await act(async () => {});
  mock.snapshot.mockClear();
  return subject;
}
async function invalidate() {
  await act(async () => mock.listener());
}
async function opening(revision: number) {
  await act(async () =>
    window.dispatchEvent(
      new CustomEvent("popchat:panel-opening", {
        detail: { revision, pending: true },
      }),
    ),
  );
}
it("coalesces 100 stream changes while the previous snapshot is still in flight", async () => {
  await setup();
  mock.snapshot.mockReturnValue(new Promise(() => {}));
  await invalidate();
  await waitFor(() => expect(mock.snapshot).toHaveBeenCalledTimes(1));
  await act(async () => {
    for (let i = 0; i < 100; i++) mock.listener();
  });
  expect(mock.snapshot).toHaveBeenCalledTimes(1);
});
it("refreshes the final state once after an in-flight burst", async () => {
  await setup();
  let finish!: (s: Snapshot) => void;
  mock.snapshot
    .mockReturnValueOnce(
      new Promise<Snapshot>((resolve) => {
        finish = resolve;
      }),
    )
    .mockResolvedValue(state(3));
  await invalidate();
  await waitFor(() => expect(mock.snapshot).toHaveBeenCalledTimes(1));
  await act(async () => {
    for (let i = 0; i < 100; i++) mock.listener();
    finish(state(2));
  });
  await screen.findByText("synthetic-3");
  expect(mock.snapshot).toHaveBeenCalledTimes(2);
});
it("discards pending work on unmount", async () => {
  const subject = await setup();
  let finish!: (s: Snapshot) => void;
  mock.snapshot.mockReturnValueOnce(
    new Promise<Snapshot>((resolve) => {
      finish = resolve;
    }),
  );
  await invalidate();
  await waitFor(() => expect(mock.snapshot).toHaveBeenCalledTimes(1));
  await invalidate();
  subject.unmount();
  await act(async () => finish(state(2)));
  expect(mock.snapshot).toHaveBeenCalledTimes(1);
});
it("does not let an older opening restore input during a newer panel invocation", async () => {
  await setup();
  let first!: (s: Snapshot) => void, second!: (s: Snapshot) => void;
  mock.snapshot
    .mockReturnValueOnce(
      new Promise<Snapshot>((r) => {
        first = r;
      }),
    )
    .mockReturnValueOnce(
      new Promise<Snapshot>((r) => {
        second = r;
      }),
    )
    .mockResolvedValue(state(3, 2));
  const input = screen.getByRole("textbox", {
    name: "消息",
  }) as HTMLTextAreaElement;
  await opening(1);
  expect(input.disabled).toBe(true);
  await waitFor(() => expect(mock.snapshot).toHaveBeenCalledTimes(1));
  await opening(2);
  await act(async () => first(state(2, 1)));
  await waitFor(() => expect(mock.snapshot).toHaveBeenCalledTimes(2));
  expect(input.disabled).toBe(true);
  fireEvent.keyDown(input, { key: "Enter", keyCode: 13 });
  expect(mock.action).not.toHaveBeenCalled();
  await act(async () => second(state(3, 2)));
  await waitFor(() => expect(input.disabled).toBe(false));
  await opening(1);
  await opening(2);
  expect(input.disabled).toBe(false);
});
it("rechecks a pending panel even if its ready notification was missed", async () => {
  mock.snapshot
    .mockResolvedValueOnce(state(1, 1, true))
    .mockResolvedValue(state(2, 1, false));
  render(<App />);
  await screen.findByText("synthetic-2");
  expect(
    (screen.getByRole("textbox", { name: "消息" }) as HTMLTextAreaElement)
      .disabled,
  ).toBe(false);
  expect(mock.snapshot).toHaveBeenCalledTimes(2);
});
it("hides immediately while the draft save is still pending", async () => {
  await setup();
  mock.action.mockImplementation((action: string) =>
    action === "draft" ? new Promise(() => {}) : Promise.resolve(state(2)),
  );
  const input = screen.getByRole("textbox", { name: "消息" });
  fireEvent.change(input, { target: { value: "synthetic unsaved draft" } });
  await waitFor(
    () =>
      expect(mock.action).toHaveBeenCalledWith(
        "draft",
        expect.objectContaining({ id: "synthetic" }),
      ),
    { timeout: 1500 },
  );
  fireEvent.keyDown(input, { key: "Escape", keyCode: 27 });
  await waitFor(() => expect(mock.action).toHaveBeenCalledWith("hide", {}), {
    timeout: 200,
  });
});
it("starts saving the original draft immediately on hide without waiting for storage", async () => {
  await setup();
  mock.action.mockImplementation((action: string) =>
    action === "draft" ? new Promise(() => {}) : Promise.resolve(state(2)),
  );
  const input = screen.getByRole("textbox", { name: "消息" });
  fireEvent.change(input, { target: { value: "synthetic immediate draft" } });
  fireEvent.keyDown(input, { key: "Escape", keyCode: 27 });
  await waitFor(
    () => {
      expect(mock.action).toHaveBeenCalledWith("hide", {});
      expect(mock.action).toHaveBeenCalledWith(
        "draft",
        expect.objectContaining({
          id: "synthetic",
          text: "synthetic immediate draft",
        }),
      );
    },
    { timeout: 200 },
  );
});
it("finishes saving the original hidden draft after a pending save and a session switch", async () => {
  await setup();
  let finish!: (s: Snapshot) => void;
  const other = state(3);
  other.currentId = "other";
  other.current!.id = "other";
  other.current!.draft = "other draft";
  mock.action.mockImplementation(
    (action: string, payload: { text?: string }) => {
      if (action === "draft" && payload.text === "first draft")
        return new Promise<Snapshot>((resolve) => {
          finish = resolve;
        });
      return Promise.resolve(
        action === "draft" ? { ...other, version: 5 } : state(),
      );
    },
  );
  const input = screen.getByRole("textbox", { name: "消息" });
  fireEvent.change(input, { target: { value: "first draft" } });
  await waitFor(
    () =>
      expect(mock.action).toHaveBeenCalledWith(
        "draft",
        expect.objectContaining({ text: "first draft" }),
      ),
    { timeout: 1500 },
  );
  fireEvent.change(input, { target: { value: "latest original draft" } });
  fireEvent.keyDown(input, { key: "Escape", keyCode: 27 });
  mock.snapshot.mockResolvedValue(other);
  await invalidate();
  await waitFor(() =>
    expect((input as HTMLTextAreaElement).value).toBe("other draft"),
  );
  await act(async () => finish({ ...other, version: 4 }));
  await waitFor(
    () =>
      expect(mock.action).toHaveBeenCalledWith(
        "draft",
        expect.objectContaining({
          id: "synthetic",
          text: "latest original draft",
          expectedDraft: "first draft",
          expectedDraftRevision: 1,
        }),
      ),
    { timeout: 300 },
  );
  expect(
    mock.action.mock.calls
      .filter(([action]) => action === "draft")
      .every(([, payload]) => payload.id === "synthetic"),
  ).toBe(true);
});
