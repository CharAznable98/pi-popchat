// @vitest-environment jsdom
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import {
  AgentProgress,
  ProcessRecord,
  groupSteps,
  type Step,
} from "./ProcessRecord";
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

it("counts from actual dispatch and excludes time spent queued", () => {
  const clock = Date.now();
  render(
    <AgentProgress
      status="running"
      messages={[
        {
          id: "q",
          role: "user",
          text: "synthetic",
          status: "sending",
          createdAt: new Date(clock - 600000).toISOString(),
          deliveryStartedAt: new Date(clock - 2000).toISOString(),
          attachments: [],
        },
      ]}
    />,
  );
  expect(screen.getByText("等待 Agent 响应 · 2 秒")).toBeTruthy();
});
it("does not show a fabricated elapsed time before dispatch", () => {
  render(
    <AgentProgress
      status="starting"
      messages={[
        {
          id: "q",
          role: "user",
          text: "synthetic",
          status: "sending",
          createdAt: "2000-01-01T00:00:00Z",
          attachments: [],
        },
      ]}
    />,
  );
  expect(screen.getByText("正在准备投递")).toBeTruthy();
});
