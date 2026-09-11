import { useEffect, useState } from "react";
import type { Message, ProcessStep } from "../types";
export type Step = ProcessStep;
export function groupSteps(steps: Step[]): Step[][] {
  const groups: Step[][] = [];
  for (const step of steps) {
    const last = groups.at(-1);
    if (last?.[0].action === step.action) last.push(step);
    else groups.push([step]);
  }
  return groups;
}
const statusName: Record<string, string> = {
  running: "进行中",
  completed: "已完成",
  failed: "失败",
  interrupted: "已中断",
  pending: "等待中",
};
export function ProcessRecord({
  message,
  active,
  failed,
}: {
  message: Message;
  active: boolean;
  failed: boolean;
}) {
  const steps = message.steps ?? [];
  const [expanded, setExpanded] = useState(active || failed);
  useEffect(() => setExpanded(active || failed), [active, failed]);
  if (!steps.length) return null;
  const failures = steps.filter((s) => s.status === "failed").length;
  return (
    <section className="process-record" aria-label="过程记录">
      <button
        className="process-summary"
        aria-expanded={expanded}
        onClick={() => setExpanded(!expanded)}
      >
        {expanded ? "▾" : "▸"} 过程记录 · {steps.length} 步
        {failures > 0 ? ` · ${failures} 步失败` : ""}
      </button>
      {expanded &&
        groupSteps(steps).map((group) => {
          const status = group.some((s) => s.status === "running")
            ? "running"
            : group.some((s) => s.status === "pending")
              ? "pending"
              : group.some((s) => s.status === "failed")
                ? "failed"
                : group.some((s) => s.status === "interrupted")
                  ? "interrupted"
                  : "completed";
          return (
            <details key={group[0].id} className={`process-group ${status}`}>
              <summary>
                {group[0].action}
                {group.length > 1 ? ` × ${group.length}` : ""}
                <span>{statusName[status]}</span>
              </summary>
              <ul>
                {group.map((step) => (
                  <li key={step.id}>
                    <span>{step.object || step.action}</span>
                    <small>
                      {statusName[step.status]}
                      {step.endedAt && step.startedAt
                        ? ` · ${Math.max(0, Math.round((Date.parse(step.endedAt) - Date.parse(step.startedAt)) / 1000))} 秒`
                        : ""}
                    </small>
                  </li>
                ))}
              </ul>
            </details>
          );
        })}
    </section>
  );
}
export function AgentProgress({
  messages,
  status,
}: {
  messages: Message[];
  status: string;
}) {
  const [tick, setTick] = useState(Date.now());
  useEffect(() => {
    const timer = setInterval(() => setTick(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  let lastUser = -1;
  messages.forEach((m, i) => {
    if (m.role === "user") lastUser = i;
  });
  const current = messages.slice(lastUser + 1);
  const running = messages[lastUser]?.steps
    ?.filter((s) => s.status === "running")
    .at(-1);
  let label = status === "retrying" ? "正在重试" : running?.action;
  if (!label)
    label = current.some(
      (m) => m.role === "assistant" && m.status === "sending" && m.text,
    )
      ? "正在生成回答"
      : `等待 Agent 响应 · ${Math.max(0, Math.floor((tick - Date.parse(messages[lastUser]?.createdAt ?? new Date(tick).toISOString())) / 1000))} 秒`;
  return <>{label}</>;
}
