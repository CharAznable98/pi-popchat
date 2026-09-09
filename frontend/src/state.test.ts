import { test } from "node:test";
import assert from "node:assert/strict";
import { newer, shouldSend, fileTarget, isBusy } from "./state.ts";
test("旧事件返回不能覆盖新快照", () => {
  let s = { version: 9 } as any;
  assert.equal(newer(s, { version: 8 } as any), s);
  assert.equal(newer(s, { version: 10 } as any).version, 10);
});
test("中文组合输入、Shift Enter 不发送", () => {
  assert.equal(shouldSend("Enter", false, true, 13), false);
  assert.equal(shouldSend("Enter", false, false, 229), false);
  assert.equal(shouldSend("Enter", true, false, 13), false);
  assert.equal(shouldSend("Enter", false, false, 13), true);
});
test("文件链接使用会话目录，禁止非预期协议", () => {
  assert.deepEqual(fileTarget("report%20one.md", "/work"), {
    action: "openFile",
    payload: { path: "/work/report one.md" },
  });
  assert.equal(fileTarget("javascript:alert(1)", "/work"), null);
  assert.deepEqual(fileTarget("https://example.com", "/work"), {
    action: "openURL",
    payload: { url: "https://example.com" },
  });
});
test("等待交互仍是执行中，失败不算执行中", () => {
  assert.equal(isBusy("waiting"), true);
  assert.equal(isBusy("failed"), false);
});
