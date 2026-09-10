import { useState } from "react";
import { DeleteSessionDialog } from "./DeleteSessionDialog";
import type { Session, UIAction } from "../types";
import { isBusy } from "../state";
import { labels } from "./status";
export function HistorySidebar({
  sessions,
  currentId,
  search,
  setSearch,
  menu,
  setMenu,
  shortcut,
  onSettings,
  run,
  navigate,
  onDelete,
}: {
  sessions: Session[];
  currentId?: string;
  search: string;
  setSearch: (value: string) => void;
  menu: string;
  setMenu: (value: string) => void;
  shortcut?: string;
  onSettings: () => void;
  run: UIAction;
  navigate: UIAction;
  onDelete: (id: string, removeWorkspace?: boolean) => Promise<unknown>;
}) {
  const [deleting, setDeleting] = useState<Session | null>(null);
  return (
    <aside className="sidebar">
      <div className="brand">
        <img className="brand-mark" src="/popchat.svg" alt="" />
        <span>
          Popchat<small>随时开始，自在继续</small>
        </span>
      </div>
      <button className="new-chat" onClick={() => void navigate("new")}>
        <span>＋</span> 新对话 <kbd>⌘ N</kbd>
      </button>
      <input
        className="search"
        aria-label="搜索历史会话"
        placeholder="搜索历史会话"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
      />
      <div className="history">
        <small className="section-label">你的会话</small>
        {sessions.map((s) => (
          <div
            key={s.id}
            className={"session " + (currentId === s.id ? "selected" : "")}
          >
            <button
              className="session-select"
              onClick={() => void navigate("select", { id: s.id })}
            >
              <span className={"dot " + s.status} />
              <span className="session-title">
                {s.pinned ? "⌃ " : ""}
                {s.title || "新对话"}
                <small>{labels[s.status] || s.status}</small>
              </span>
            </button>
            <button
              className="more"
              aria-label={"管理 " + s.title}
              onClick={() => setMenu(menu === s.id ? "" : s.id)}
            >
              ⋯
            </button>
            {menu === s.id && (
              <div className="session-menu">
                <button
                  onClick={() => {
                    const title = window.prompt("会话名称", s.title);
                    if (title?.trim())
                      run("rename", { id: s.id, title: title.trim() });
                    setMenu("");
                  }}
                >
                  重命名
                </button>
                <button
                  onClick={() => {
                    run("pin", { id: s.id, pinned: !s.pinned });
                    setMenu("");
                  }}
                >
                  {s.pinned ? "取消置顶" : "置顶"}
                </button>
                <button
                  className="danger"
                  disabled={isBusy(s.status)}
                  onClick={() => {
                    setDeleting(s);
                    setMenu("");
                  }}
                >
                  删除会话
                </button>
              </div>
            )}
          </div>
        ))}
        {!sessions.length && (
          <p className="muted empty-history">没有匹配的会话</p>
        )}
      </div>
      <button className="settings-button" onClick={onSettings}>
        ⚙ 设置 <span>{shortcut?.replace("Alt", "⌥")}</span>
      </button>
      {deleting && (
        <DeleteSessionDialog title={deleting.title} workspace={deleting.managedWorkspace ? deleting.cwd : undefined}
          onDelete={(removeWorkspace) => onDelete(deleting.id, removeWorkspace)}
          onClose={() => setDeleting(null)} />
      )}
    </aside>
  );
}
