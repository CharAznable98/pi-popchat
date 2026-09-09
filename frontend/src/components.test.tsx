// @vitest-environment jsdom
import { useState } from "react";
import { afterEach, it, expect, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { SettingsDialog } from "./components/SettingsDialog";
afterEach(cleanup);
function Host() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <main>
        <button onClick={() => setOpen(true)}>打开设置</button>
        <input aria-label="背景输入" />
      </main>
      {open && (
        <SettingsDialog
          shortcut="Alt+Space"
          setShortcut={() => {}}
          piPath=""
          setPiPath={() => {}}
          environment={{
            available: true,
            version: "0.84.1",
            piPath: "/pi",
            error: "",
          }}
          onClose={() => setOpen(false)}
          onSave={vi.fn().mockResolvedValue({})}
          run={() => {}}
        />
      )}
    </>
  );
}
it("设置打开后聚焦第一输入，背景不可交互；Escape关闭后还原焦点", () => {
  render(<Host />);
  const opener = screen.getByRole("button", { name: "打开设置" });
  opener.focus();
  fireEvent.click(opener);
  const first = screen.getByRole("textbox", { name: "全局唤起快捷键" });
  expect(document.activeElement).toBe(first);
  expect(opener.closest("main")!.inert).toBe(true);
  fireEvent.keyDown(first, { key: "Escape" });
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(document.activeElement).toBe(opener);
  expect(opener.closest("main")!.inert).toBeFalsy();
});
it("设置Tab与Shift Tab限制在对话框内，组合输入Escape不关闭", () => {
  render(<Host />);
  fireEvent.click(screen.getByRole("button", { name: "打开设置" }));
  const first = screen.getByRole("button", { name: "关闭设置" }),
    last = screen.getByRole("button", { name: "退出应用" });
  last.focus();
  fireEvent.keyDown(last, { key: "Tab" });
  expect(document.activeElement).toBe(first);
  fireEvent.keyDown(first, { key: "Tab", shiftKey: true });
  expect(document.activeElement).toBe(last);
  fireEvent.keyDown(last, { key: "Escape", isComposing: true, keyCode: 229 });
  expect(screen.getByRole("dialog")).toBeTruthy();
});
