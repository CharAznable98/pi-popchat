import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
export function DeleteSessionDialog({ title, onDelete, onClose }: {
  title: string;
  onDelete: () => Promise<unknown>;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const locked = useRef(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const previous = document.activeElement;
    dialog.current?.showModal();
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus();
    };
  }, []);
  const remove = async () => {
    if (locked.current) return;
    locked.current = true;
    setPending(true);
    setError("");
    try {
      await onDelete();
      onClose();
    } catch (error) {
      setError(String(error));
    } finally {
      locked.current = false;
      setPending(false);
    }
  };
  return createPortal(
    <dialog ref={dialog} className="modal delete-session-dialog" role="alertdialog"
      aria-labelledby="delete-session-title" aria-describedby="delete-session-description"
      onCancel={(event) => { event.preventDefault(); if (!locked.current) onClose(); }}
      onKeyDown={(event) => {
        event.stopPropagation();
        if (event.key === "Escape" && (event.nativeEvent.isComposing || event.keyCode === 229)) event.preventDefault();
      }}>
      <header><h2 id="delete-session-title">删除会话？</h2></header>
      <p id="delete-session-description">将删除“{title || "新对话"}”的会话记录。工作目录中的文件将保留。</p>
      {error && <p role="alert" className="task-error">{error}</p>}
      <footer>
        <button autoFocus disabled={pending} onClick={onClose}>取消</button>
        <button className="danger" disabled={pending} onClick={() => void remove()}>
          {pending ? "删除中…" : "确认删除"}
        </button>
      </footer>
    </dialog>, document.body,
  );
}
