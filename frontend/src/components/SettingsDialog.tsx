import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import type { Snapshot, UIAction } from "../types";
export function SettingsDialog({
  shortcut,
  setShortcut,
  piPath,
  setPiPath,
  environment,
  onClose,
  onSave,
  run,
}: {
  shortcut: string;
  setShortcut: (value: string) => void;
  piPath: string;
  setPiPath: (value: string) => void;
  environment?: Snapshot["environment"];
  onClose: () => void;
  onSave: (settings: Snapshot["settings"]) => Promise<unknown>;
  run: UIAction;
}) {
  const dialog = useRef<HTMLElement>(null),
    savingLock = useRef(false);
  const [saving, setSaving] = useState(false),
    [saveError, setSaveError] = useState("");
  const save = async () => {
    if (savingLock.current) return;
    savingLock.current = true;
    setSaving(true);
    setSaveError("");
    try {
      await onSave({ shortcut, piPath });
      onClose();
    } catch (error) {
      setSaveError(String(error));
    } finally {
      savingLock.current = false;
      setSaving(false);
    }
  };
  useEffect(() => {
    const previous =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    const backdrop = dialog.current?.parentElement;
    const siblings = Array.from(backdrop?.parentElement?.children ?? []).filter(
      (node): node is HTMLElement =>
        node instanceof HTMLElement && node !== backdrop,
    );
    const wasInert = siblings.map((node) => node.inert);
    siblings.forEach((node) => {
      node.inert = true;
    });
    dialog.current?.querySelector<HTMLInputElement>("input")?.focus();
    return () => {
      siblings.forEach((node, index) => {
        node.inert = wasInert[index];
      });
      if (previous?.isConnected) previous.focus();
    };
  }, []);
  const handleKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.nativeEvent.isComposing || event.keyCode === 229) return;
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      onClose();
      return;
    }
    if (event.key !== "Tab") return;
    const focusable = Array.from(
      dialog.current?.querySelectorAll<HTMLElement>(
        'button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),a[href],[tabindex="0"]',
      ) ?? [],
    );
    const first = focusable[0],
      last = focusable.at(-1);
    if (!first) {
      event.preventDefault();
      return;
    }
    if (
      event.shiftKey &&
      (document.activeElement === first ||
        !dialog.current?.contains(document.activeElement))
    ) {
      event.preventDefault();
      last?.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  };
  return (
    <div className="modal-backdrop" onClick={() => onClose()}>
      <section
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-label="设置"
        ref={dialog}
        onKeyDown={handleKeyDown}
        onClick={(e) => e.stopPropagation()}
      >
        <header>
          <h2>设置</h2>
          <button aria-label="关闭设置" onClick={() => onClose()}>
            ×
          </button>
        </header>
        <label>
          全局唤起快捷键
          <input
            value={shortcut}
            onChange={(e) => setShortcut(e.target.value)}
            placeholder="Alt+Space"
          />
        </label>
        <small>例如 Alt+Space、Ctrl+Shift+Space。冲突会提示。</small>
        <label>
          Pi 可执行文件路径
          <input
            value={piPath}
            onChange={(e) => setPiPath(e.target.value)}
            placeholder="留空自动检测"
          />
        </label>
        <small>模型与登录信息由 Pi 管理。</small>
        <div className="environment">
          {environment?.available ? "Pi 已安装" : "尚未找到 Pi"} · Pi{" "}
          {environment?.version || "未检测"}
          <button onClick={() => run("refreshAgent")}>重新检测</button>
        </div>
        <p className="muted" style={{ fontSize: 11 }}>
          应用不保存模型凭证。请按 Pi
          官方指引完成登录或配置；模型列表可见不代表凭证有效。
        </p>
        <button
          onClick={() =>
            run("openURL", {
              url: "https://github.com/earendil-works/pi/tree/main/packages/coding-agent",
            })
          }
        >
          打开 Pi 配置说明 ↗
        </button>
        {saveError && (
          <p role="alert" className="task-error">
            {saveError}
          </p>
        )}
        <button
          className="primary"
          disabled={saving}
          onClick={() => void save()}
        >
          {saving ? "保存中…" : "保存设置"}
        </button>
        <button className="quit" onClick={() => run("quit")}>
          退出应用
        </button>
      </section>
    </div>
  );
}
