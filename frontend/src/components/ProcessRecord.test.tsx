// @vitest-environment jsdom
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { ProcessRecord, groupSteps, type Step } from "./ProcessRecord";
afterEach(cleanup);
const step = (id: string, action: string, status = "completed"): Step => ({
  id,
  action,
  status,
  object: `synthetic-${id}`,
  startedAt: "2026-09-11T00:00:00Z",
  endedAt: "2026-09-11T00:00:02Z",
});
it("merges only consecutive identical actions", () => {
  expect(
    groupSteps([
      step("1", "读取文件"),
      step("2", "读取文件"),
      step("3", "执行命令"),
      step("4", "读取文件"),
    ]).map((g) => g.length),
  ).toEqual([2, 1, 1]);
});
it("collapses completed records, exposes failures and object details", () => {
  const message = {
    id: "u",
    role: "user",
    text: "synthetic",
    status: "complete",
    createdAt: "",
    attachments: [],
    steps: [step("1", "读取文件", "failed")],
  };
  const { rerender } = render(
    <ProcessRecord message={message} active={true} failed={false} />,
  );
  expect(screen.getByText("synthetic-1")).toBeTruthy();
  rerender(<ProcessRecord message={message} active={false} failed={false} />);
  expect(screen.queryByText("synthetic-1")).toBeNull();
  fireEvent.click(screen.getByRole("button"));
  expect(screen.getByText("synthetic-1")).toBeTruthy();
  rerender(<ProcessRecord message={message} active={false} failed={true} />);
  expect(screen.getByText("synthetic-1")).toBeTruthy();
});
