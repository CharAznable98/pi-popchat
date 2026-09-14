// @vitest-environment jsdom
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { SelectionSettings, defaultSelection } from "./SelectionSettings";
afterEach(cleanup);
it("allows deleting defaults and restoring them", () => {
  const change = vi.fn();
  const { rerender } = render(
    <SelectionSettings
      value={defaultSelection()}
      onChange={change}
      requestPermission={() => {}}
    />,
  );
  fireEvent.click(screen.getAllByText("删除")[0]);
  expect(
    change.mock.calls[0][0].buttons.map((b: { id: string }) => b.id),
  ).toEqual(["explain"]);
  rerender(
    <SelectionSettings
      value={{ enabled: false, buttons: [] }}
      onChange={change}
      requestPermission={() => {}}
    />,
  );
  expect(screen.getByText("没有按钮时不显示工具条。")).toBeTruthy();
  fireEvent.click(screen.getByText("恢复默认按钮"));
  expect(change.mock.calls.at(-1)?.[0]).toEqual({
    ...defaultSelection(),
    enabled: false,
  });
});
it("warns without deleting unknown placeholders", () => {
  const change = vi.fn();
  render(
    <SelectionSettings
      value={{
        enabled: true,
        buttons: [{ id: "custom", name: "custom", template: "{{unknown}}" }],
      }}
      onChange={change}
      requestPermission={() => {}}
    />,
  );
  expect(screen.getByRole("status").textContent).toContain("unknown");
  expect(change).not.toHaveBeenCalled();
});
