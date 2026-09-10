import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
} from "react";

import { api, view } from "./api";
import type { Attachment, Snapshot } from "./types";
import { newer, isBusy, shouldSend, fileTarget } from "./state";
import "./style.css";
import { InteractionCard } from "./components/AgentContent";
import { HistorySidebar } from "./components/HistorySidebar";
import { MessageView } from "./components/MessageView";
import { SettingsDialog } from "./components/SettingsDialog";
import { labels } from "./components/status";
export function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null),
    [error, setError] = useState(""),
    [text, setText] = useState(""),
    [attachments, setAttachments] = useState<Attachment[]>([]),
    [sending, setSending] = useState(false),
    [uploading, setUploading] = useState(false),
    [search, setSearch] = useState(""),
    [settings, setSettings] = useState(false),
    [shortcut, setShortcut] = useState(""),
    [piPath, setPiPath] = useState(""),
    [menu, setMenu] = useState(""),
    [drag, setDrag] = useState(false),
    [conflict, setConflict] = useState(false),
    [commandsDismissed, setCommandsDismissed] = useState(false),
    [commandIndex, setCommandIndex] = useState(0);
  const stateRef = useRef(snapshot),
    draft = useRef(""),
    attachmentRef = useRef<Attachment[]>([]),
    persistLock = useRef<Promise<boolean> | null>(null),
    base = useRef(""),
    draftRevision = useRef(0),
    dirty = useRef(false),
    compose = useRef(false),
    sessionId = useRef(""),
    input = useRef<HTMLTextAreaElement>(null),
    bottom = useRef<HTMLDivElement>(null),
    followOutput = useRef(true),
    fileInput = useRef<HTMLInputElement>(null),
    sendLock = useRef(false),
    pendingSends = useRef(new Map<string, {
      id: string;
      text: string;
      attachments: Attachment[];
    }>()),
    draftTimer = useRef<ReturnType<typeof setTimeout> | null>(null),
    localDrafts = useRef(
      new Map<
        string,
        {
          text: string;
          base: string;
          attachments: Attachment[];
          revision: number;
        }
      >(),
    );
  const apply = useCallback((s: Snapshot) => {
    const accepted = newer(stateRef.current, s);
    if (accepted !== s) return;
    stateRef.current = s;
    setSnapshot(s);
    const c = s.current;
    if (c?.id !== sessionId.current) {
      sessionId.current = c?.id || "";
      followOutput.current = true;
      const local = c && localDrafts.current.get(c.id);
      draft.current = local?.text ?? c?.draft ?? "";
      base.current = local?.base ?? c?.draft ?? "";
      draftRevision.current = local?.revision ?? c?.draftRevision ?? 0;
      dirty.current = !!local;
      setText(draft.current);
      attachmentRef.current = local?.attachments ?? c?.draftAttachments ?? [];
      setAttachments(attachmentRef.current);
      setConflict(false);
      setTimeout(() => input.current?.focus(), 0);
    } else if (c && !dirty.current) {
      draft.current = c.draft || "";
      base.current = draft.current;
      draftRevision.current = c.draftRevision ?? 0;
      setText(draft.current);
      attachmentRef.current = c.draftAttachments ?? [];
      setAttachments(attachmentRef.current);
    }
  }, []);
  const refresh = useCallback(
    () =>
      api
        .snapshot()
        .then(apply)
        .catch((e) => setError(String(e))),
    [apply],
  );
  const act = useCallback(
    async (action: string, payload: Record<string, unknown> = {}) => {
      try {
        const scoped = new Set([
          "stop",
          "insert",
          "removeQueued",
          "resumeQueue",
          "pauseQueue",
          "respond",
          "model",
          "chooseDirectory",
          "openFile",
          "revealFile",
          "refreshAgent",
        ]);
        const actionPayload = scoped.has(action)
          ? { sessionId: stateRef.current?.currentId, ...payload }
          : payload;
        const s = await api.action(action, actionPayload);
        apply(s);
        setError("");
        return s;
      } catch (e) {
        setError(String(e));
        throw e;
      }
    },
    [apply],
  );
  const run = (action: string, payload: Record<string, unknown> = {}) => {
    void act(action, payload).catch(() => {});
  };
  useEffect(() => {
    void refresh();
    const off = api.subscribe(() => void refresh());
    const focus = () => void refresh();
    window.addEventListener("focus", focus);
    return () => {
      off();
      if (draftTimer.current) clearTimeout(draftTimer.current);
      window.removeEventListener("focus", focus);
    };
  }, [refresh]);
  const current = snapshot?.current,
    busy = isBusy(current?.status || "");
  useEffect(() => {
    if (followOutput.current)
      bottom.current?.scrollIntoView({ behavior: "auto" });
  }, [
    current?.messages?.at(-1)?.text,
    current?.queue?.length,
    current?.interaction?.id,
  ]);
  const persist = async (): Promise<boolean> => {
    if (persistLock.current) {
      await persistLock.current;
      return persist();
    }
    if (!dirty.current || !sessionId.current || sendLock.current) return true;
    const id = sessionId.current,
      value = draft.current,
      expected = base.current,
      files = attachmentRef.current;
    const operation = (async () => {
      try {
        const s = await api.action("draft", {
          text: value,
          expectedDraft: expected,
          expectedDraftRevision: draftRevision.current,
          id,
          attachments: files,
        });
        if (sessionId.current === id) {
          base.current = value;
          draftRevision.current =
            s.current?.id === id
              ? (s.current.draftRevision ?? draftRevision.current + 1)
              : draftRevision.current + 1;
          if (draft.current === value && attachmentRef.current === files) {
            dirty.current = false;
            setConflict(false);
            localDrafts.current.delete(id);
          }
        }
        apply(s);
        return true;
      } catch {
        if (sessionId.current === id) setConflict(true);
        return false;
      }
    })();
    persistLock.current = operation;
    try {
      return await operation;
    } finally {
      persistLock.current = null;
    }
  };
  const updateAttachments = (next: Attachment[]) => {
    attachmentRef.current = next;
    setAttachments(next);
    dirty.current = true;
    localDrafts.current.set(sessionId.current, {
      text: draft.current,
      base: base.current,
      revision: draftRevision.current,
      attachments: next,
    });
    if (draftTimer.current) clearTimeout(draftTimer.current);
    draftTimer.current = setTimeout(() => void persist(), 650);
  };
  const changeText = (value: string) => {
    draft.current = value;
    setCommandsDismissed(false);
    setCommandIndex(0);
    dirty.current = true;
    setText(value);
    localDrafts.current.set(sessionId.current, {
      text: value,
      base: base.current,
      revision: draftRevision.current,
      attachments: attachmentRef.current,
    });
    if (draftTimer.current) clearTimeout(draftTimer.current);
    draftTimer.current = setTimeout(() => void persist(), 650);
  };
  const navigate = async (
    action: string,
    payload: Record<string, unknown> = {},
  ) => {
    if (draftTimer.current) clearTimeout(draftTimer.current);
    await persist();
    run(action, payload);
  };
  const send = async () => {
    if (
      sendLock.current ||
      uploading ||
      (!draft.current.trim() && !attachments.length) ||
      !current
    )
      return;
    const sendingSession = current.id;
    const sendingRevision = draftRevision.current;
    const sendingBase = base.current;
    followOutput.current = true;
    sendLock.current = true;
    setSending(true);
    if (draftTimer.current) clearTimeout(draftTimer.current);
    const content = draft.current;
    const files = attachmentRef.current;
    const candidate = pendingSends.current.get(sendingSession);
    const transaction =
      candidate &&
      candidate.text === content &&
      JSON.stringify(candidate.attachments) === JSON.stringify(files)
        ? candidate
        : { id: crypto.randomUUID(), text: content, attachments: files };
    pendingSends.current.set(sendingSession, transaction);
    try {
      if (persistLock.current) await persistLock.current;
      // Even an already saved draft belongs to this pending submission. Keep
      // it locally until success, so notifications and conflict refreshes cannot
      // replace its text or attachments with another window's draft.
      const stillSelected = sessionId.current === sendingSession;
      if (!localDrafts.current.has(sendingSession)) {
        localDrafts.current.set(sendingSession, {
          text: transaction.text,
          attachments: transaction.attachments,
          base: stillSelected ? base.current : sendingBase,
          revision: stillSelected ? draftRevision.current : sendingRevision,
        });
      }
      if (stillSelected) dirty.current = true;
      const result = await act("send", {
        text: transaction.text,
        attachments: transaction.attachments,
        clientMessageId: transaction.id,
        expectedDraftRevision: sessionId.current === sendingSession ? draftRevision.current : sendingRevision,
        id: sendingSession,
      });
      if (sessionId.current === sendingSession) {
        base.current = result.current?.draft ?? "";
        draftRevision.current =
          result.current?.draftRevision ?? draftRevision.current + 1;
      }
      if (
        sessionId.current === sendingSession &&
        draft.current === content &&
        attachmentRef.current === files
      ) {
        draft.current = "";
        base.current = "";
        dirty.current = false;
        setText("");
        attachmentRef.current = [];
        setAttachments([]);
        localDrafts.current.delete(current.id);
        setConflict(false);
      }
      const local = localDrafts.current.get(sendingSession);
      if (local?.text === transaction.text &&
          JSON.stringify(local.attachments) === JSON.stringify(transaction.attachments)) {
        localDrafts.current.delete(sendingSession);
      }
      if (pendingSends.current.get(sendingSession) === transaction) {
        pendingSends.current.delete(sendingSession);
      }
    } catch (error) {
      if (String(error).includes("另一窗口已更新或发送草稿")) {
        if (sessionId.current === sendingSession) setConflict(true);
        void refresh();
      }
    } finally {
      sendLock.current = false;
      setSending(false);
      if (dirty.current)
        draftTimer.current = setTimeout(() => void persist(), 650);
      input.current?.focus();
    }
  };
  const addFiles = async (files: File[]) => {
    const uploadSession = sessionId.current;
    const origin = {
      text: draft.current,
      base: base.current,
      revision: draftRevision.current,
      attachments: attachmentRef.current,
    };
    setUploading(true);
    try {
      const added: Attachment[] = [];
      for (const file of files) {
        if (file.size > 32 * 1024 * 1024) throw Error("单个附件不能超过 32 MB");
        const bytes = new Uint8Array(await file.arrayBuffer());
        let raw = "";
        for (let i = 0; i < bytes.length; i += 8192)
          raw += String.fromCharCode(...bytes.subarray(i, i + 8192));
        const attachment = await api.save(
          file.name,
          file.type || "application/octet-stream",
          btoa(raw),
        );
        if (file.type.startsWith("image/"))
          attachment.preview = "data:" + file.type + ";base64," + btoa(raw);
        added.push(attachment);
      }
      if (sessionId.current === uploadSession)
        updateAttachments([...attachmentRef.current, ...added]);
      else {
        const local = localDrafts.current.get(uploadSession) ?? origin;
        const saved = {
          ...local,
          attachments: [...local.attachments, ...added],
        };
        localDrafts.current.set(uploadSession, saved);
        const result = await api.action("draft", {
          id: uploadSession,
          text: saved.text,
          expectedDraft: saved.base,
          expectedDraftRevision: saved.revision,
          attachments: saved.attachments,
        });
        localDrafts.current.delete(uploadSession);
        apply(result);
      }
    } catch (e) {
      setError(String(e));
    } finally {
      setUploading(false);
    }
  };
  const keyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (compose.current || e.nativeEvent.isComposing || e.keyCode === 229)
      return;
    if (
      commands.length &&
      ["ArrowDown", "ArrowUp", "Tab", "Enter"].includes(e.key) &&
      !e.shiftKey
    ) {
      e.preventDefault();
      if (e.key === "ArrowDown")
        setCommandIndex((i) => (i + 1) % Math.min(commands.length, 8));
      else if (e.key === "ArrowUp")
        setCommandIndex(
          (i) =>
            (i + Math.min(commands.length, 8) - 1) %
            Math.min(commands.length, 8),
        );
      else {
        changeText(
          "/" +
            commands[Math.min(commandIndex, commands.length - 1)].name +
            " ",
        );
        setCommandsDismissed(true);
      }
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      if (menu) setMenu("");
      else if (text.startsWith("/") && !commandsDismissed)
        setCommandsDismissed(true);
      else if (view === "panel") void navigate("hide");
      return;
    }
    if (shouldSend(e.key, e.shiftKey, false, e.keyCode)) {
      e.preventDefault();
      void send();
    }
  };
  useEffect(() => {
    const key = (e: globalThis.KeyboardEvent) => {
      if (
        e.metaKey &&
        e.key.toLowerCase() === "n" &&
        !e.isComposing &&
        !compose.current
      ) {
        e.preventDefault();
        void navigate("new");
        return;
      }
      if (
        e.key !== "Escape" ||
        e.isComposing ||
        compose.current ||
        e.keyCode === 229 ||
        e.defaultPrevented
      )
        return;
      if (settings) {
        setSettings(false);
        e.preventDefault();
      } else if (menu) {
        setMenu("");
        e.preventDefault();
      } else if (view === "panel") {
        e.preventDefault();
        void navigate("hide");
      }
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  });
  const commands =
    !commandsDismissed && text.startsWith("/") && !text.includes("\n")
      ? (current?.commands || []).filter((c) =>
          c.name.toLowerCase().includes(text.slice(1).toLowerCase()),
        )
      : [];
  const openLink = (href: string, reveal = false) => {
    const target = fileTarget(href, current?.cwd || "");
    if (target)
      run(
        reveal && target.action === "openFile" ? "revealFile" : target.action,
        target.payload,
      );
  };
  const sessions = (snapshot?.sessions || [])
    .filter((s) =>
      (s.title + " " + (s.searchableText || ""))
        .toLowerCase()
        .includes(search.toLowerCase()),
    )
    .sort(
      (a, b) =>
        Number(b.pinned) - Number(a.pinned) ||
        b.updatedAt.localeCompare(a.updatedAt),
    );
  return (
    <div
      className={"app " + view}
      onDragOver={(e) => {
        e.preventDefault();
        setDrag(true);
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node)) setDrag(false);
      }}
      onDrop={(e) => {
        e.preventDefault();
        setDrag(false);
        void addFiles(Array.from(e.dataTransfer.files));
      }}
    >
      {view === "main" && (
        <HistorySidebar
          sessions={sessions}
          currentId={current?.id}
          search={search}
          setSearch={setSearch}
          menu={menu}
          setMenu={setMenu}
          shortcut={snapshot?.settings.shortcut}
          onSettings={() => {
            setSettings(true);
            setShortcut(snapshot?.settings.shortcut || "Alt+Space");
            setPiPath(snapshot?.settings.piPath || "");
          }}
          run={run}
          navigate={navigate}
          onDelete={(id, removeWorkspace) => act("delete", { id, ...(removeWorkspace ? { removeWorkspace: true } : {}) })}
        />
      )}
      <main className="conversation">
        <header className="topbar">
          <div>
            <strong>{current?.title || "新对话"}</strong>
            <span className={"status " + current?.status}>
              {labels[current?.status || "idle"]}
            </span>
          </div>
          <div className="top-actions">
            {view === "panel" ? (
              <>
                <button
                  title="新对话"
                  aria-label="新对话"
                  onClick={() => void navigate("new")}
                >
                  ＋
                </button>
                <button
                  title="在主窗口中打开"
                  onClick={() => void navigate("transfer")}
                >
                  ↗ 主窗口
                </button>
                <button
                  aria-label="收起浮窗"
                  onClick={() => void navigate("hide")}
                >
                  −
                </button>
              </>
            ) : (
              <button
                className="subtle"
                onClick={() => {
                  setSettings(true);
                  setShortcut(snapshot?.settings.shortcut || "Alt+Space");
                  setPiPath(snapshot?.settings.piPath || "");
                }}
              >
                设置
              </button>
            )}
          </div>
        </header>
        {(error || snapshot?.error) && (
          <div role="alert" className="error-banner">
            {error || snapshot?.error}
            <button aria-label="关闭提示" onClick={() => setError("")}>
              ×
            </button>
          </div>
        )}
        {snapshot && !snapshot.environment.available && (
          <div className="setup">
            <strong>连接你的 Agent</strong>
            <p>尚未找到可用的 Pi。请先安装并完成模型配置，然后重新检测。</p>
            <div>
              <button
                onClick={() =>
                  run("openURL", {
                    url: "https://github.com/earendil-works/pi/tree/main/packages/coding-agent",
                  })
                }
              >
                安装与配置指南 ↗
              </button>
              <button onClick={() => run("refreshAgent")}>重新检测</button>
            </div>
            {snapshot.environment.error && (
              <small>{snapshot.environment.error}</small>
            )}
          </div>
        )}
        {snapshot?.environment.available &&
          current &&
          !current.models?.length &&
          (!current.modelsState || current.modelsState === "loading") &&
          current.status !== "failed" && (
            <div className="setup" role="status">
              正在获取可用模型…
            </div>
          )}
        {snapshot?.environment.available &&
          current?.modelsState === "error" && (
            <div className="setup" role="alert">
              <strong>获取模型失败</strong>
              <p>{current.modelsError}</p>
              <button onClick={() => run("refreshAgent")}>重试</button>
            </div>
          )}
        {snapshot?.environment.available &&
          current &&
          current.modelsState === "ready" &&
          !current.models?.length &&
          !busy && (
            <div className="setup">
              <strong>尚未获取到可用模型</strong>
              <p>
                Pi 已安装。请检查 Pi 的模型配置或登录状态，配置完成后重新检测。
              </p>
              <button
                onClick={() =>
                  run("openURL", {
                    url: "https://github.com/earendil-works/pi/tree/main/packages/coding-agent",
                  })
                }
              >
                配置指南 ↗
              </button>
              <button onClick={() => run("refreshAgent")}>重新检测</button>
            </div>
          )}
        <div
          className="messages"
          onScroll={(e) => {
            const el = e.currentTarget;
            followOutput.current =
              el.scrollHeight - el.scrollTop - el.clientHeight < 90;
          }}
          aria-live="polite"
          aria-busy={busy}
        >
          {!current?.messages?.length && (
            <div className="welcome">
              <div className="welcome-symbol">✳</div>
              <h1>有什么想一起完成？</h1>
              <p>提出问题，放入文件，或从一个想法开始。</p>
              <div className="welcome-hints">
                <span>⌥ Space 随时唤起</span>
                <span>/ 使用 Agent 命令</span>
              </div>
            </div>
          )}
          {current?.messages
            ?.filter((m) => m.role !== "assistant" || m.text.trim() !== "")
            .map((m) => (
              <MessageView
                key={m.id}
                message={m}
                cwd={current.cwd}
                openLink={openLink}
                run={run}
              />
            ))}
          {busy && !current?.interaction && (
            <div className="working">
              <span className="pulse" />
              {labels[current?.status || "running"]}
              <button onClick={() => run("stop")}>停止</button>
            </div>
          )}
          {current?.error && (
            <div className="task-error" role="alert">
              {current.error}
              <button
                onClick={() => {
                  setSettings(true);
                  setShortcut(snapshot?.settings.shortcut || "Alt+Space");
                  setPiPath(snapshot?.settings.piPath || "");
                }}
              >
                配置与诊断
              </button>
            </div>
          )}
          {current?.status === "interrupted" && (
            <div className="notice">
              上次执行已中断。请确认已完成的操作后再继续；应用不会自动重发消息。
            </div>
          )}
          {current?.interaction && (
            <InteractionCard
              key={current.interaction.id}
              interaction={current.interaction}
              submit={(value, cancelled) =>
                act("respond", {
                  requestId: current.interaction!.id,
                  value,
                  cancelled,
                })
              }
            />
          )}
          <div ref={bottom} />
        </div>
        {!!current?.queue?.length && (
          <section className="queue">
            <div className="queue-heading">
              <strong>待发送 · {current.queue.length}</strong>
              <button
                onClick={() =>
                  run(
                    (current as typeof current & { queuePaused?: boolean })
                      .queuePaused
                      ? "resumeQueue"
                      : "pauseQueue",
                  )
                }
              >
                {(current as typeof current & { queuePaused?: boolean })
                  .queuePaused
                  ? "恢复队列"
                  : "暂停队列"}
              </button>
            </div>
            {current.queue.map((m) => (
              <div key={m.id} className="queue-row">
                <span>
                  {m.text || m.attachments?.map((a) => a.name).join("、")}
                  {m.status === "paused" && <small> · 已暂停</small>}
                </span>
                <button
                  disabled={!busy}
                  title="当前步骤结束后交付"
                  onClick={() => run("insert", { messageId: m.id })}
                >
                  立即插入
                </button>
                <button
                  aria-label="移除待发送消息"
                  onClick={() => run("removeQueued", { messageId: m.id })}
                >
                  ×
                </button>
              </div>
            ))}
          </section>
        )}
        <div className="composer-wrap">
          {conflict && (
            <div className="notice" role="status">
              另一窗口已更新草稿，当前输入仍保留。
              <button
                onClick={() => {
                  base.current = stateRef.current?.current?.draft || "";
                  draftRevision.current =
                    stateRef.current?.current?.draftRevision ?? 0;
                  void persist();
                }}
              >
                保留当前输入
              </button>
            </div>
          )}
          {commands.length > 0 && (
            <div className="commands" role="listbox" aria-label="Agent 命令">
              {commands.slice(0, 8).map((c, index) => (
                <button
                  key={c.name}
                  role="option"
                  aria-selected={index === commandIndex}
                  onClick={() => {
                    changeText("/" + c.name + " ");
                    input.current?.focus();
                  }}
                >
                  <strong>/{c.name}</strong>
                  <span>{c.description || c.source}</span>
                </button>
              ))}
            </div>
          )}
          <div className="composer">
            {!!attachments.length && (
              <div className="attachments">
                {attachments.map((a) => (
                  <div className="attachment" key={a.id}>
                    {a.preview && <img src={a.preview} alt={a.name} />}
                    <span>{a.name}</span>
                    <button
                      aria-label={"移除 " + a.name}
                      onClick={() =>
                        updateAttachments(
                          attachmentRef.current.filter((x) => x.id !== a.id),
                        )
                      }
                    >
                      ×
                    </button>
                  </div>
                ))}
              </div>
            )}
            <textarea
              ref={input}
              aria-label="消息"
              placeholder={
                busy ? "补充消息，将在当前任务后发送…" : "输入消息，或拖入文件…"
              }
              value={text}
              onChange={(e) => changeText(e.target.value)}
              onKeyDown={keyDown}
              onCompositionStart={() => {
                compose.current = true;
              }}
              onCompositionEnd={() => {
                compose.current = false;
              }}
              onPaste={(e) => {
                const files = Array.from(e.clipboardData.files);
                if (files.length) {
                  e.preventDefault();
                  void addFiles(files);
                }
              }}
            />
            <div className="composer-tools">
              <div>
                <button
                  aria-label="添加附件"
                  title="添加图片或文件"
                  disabled={uploading}
                  onClick={() => fileInput.current?.click()}
                >
                  ＋
                </button>
                <input
                  ref={fileInput}
                  type="file"
                  multiple
                  hidden
                  onChange={(e) => {
                    void addFiles(Array.from(e.target.files || []));
                    e.target.value = "";
                  }}
                />
                {!!current?.models?.length && (
                  <select
                    aria-label="模型"
                    value={
                      (current.provider ??
                        current.models.find((m) => m.id === current.model)
                          ?.provider ??
                        "") +
                      "/" +
                      current.model
                    }
                    disabled={busy}
                    onChange={(e) => {
                      const m = current.models.find(
                        (m) => m.provider + "/" + m.id === e.target.value,
                      );
                      if (m) run("model", { id: m.id, provider: m.provider });
                    }}
                  >
                    {current.models.map((m) => (
                      <option
                        key={m.provider + "/" + m.id}
                        value={m.provider + "/" + m.id}
                      >
                        {m.name || m.id}
                      </option>
                    ))}
                  </select>
                )}
              </div>
              <button
                className="send"
                disabled={
                  sending ||
                  uploading ||
                  (!text.trim() && !attachments.length) ||
                  !snapshot?.environment.available
                }
                onClick={() => void send()}
              >
                {uploading
                  ? "上传中"
                  : sending
                    ? "发送中"
                    : busy
                      ? "加入队列"
                      : "发送"}{" "}
                <span>↑</span>
              </button>
            </div>
          </div>
          <div className="composer-foot">
            <button
              title={current?.cwd}
              onClick={() =>
                run(
                  current?.messages?.length ? "revealFile" : "chooseDirectory",
                  current?.messages?.length ? { path: current.cwd } : {},
                )
              }
            >
              ▱{" "}
              {current?.cwd?.split("/").at(-1) === current?.id
                ? "默认工作目录"
                : current?.cwd?.split("/").at(-1) || "工作目录"}
            </button>
            <span>Enter 发送 · Shift Enter 换行</span>
          </div>
        </div>
      </main>
      {drag && <div className="drop-zone">松开放入图片或文件</div>}
      {settings && (
        <SettingsDialog
          shortcut={shortcut}
          setShortcut={setShortcut}
          piPath={piPath}
          setPiPath={setPiPath}
          environment={snapshot?.environment}
          onClose={() => setSettings(false)}
          onSave={(values) => act("settings", values)}
          run={run}
        />
      )}
    </div>
  );
}
