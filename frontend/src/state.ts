import type { Snapshot } from "./types.ts";
export function newer(previous: Snapshot | null, incoming: Snapshot): Snapshot {
  return previous && incoming.version < previous.version ? previous : incoming;
}
export function isBusy(status: string) {
  return ["starting", "running", "retrying", "waiting"].includes(status);
}
export function shouldSend(
  key: string,
  shift: boolean,
  composing: boolean,
  code: number,
) {
  return key === "Enter" && !shift && !composing && code !== 229;
}
export function fileTarget(
  href: string,
  cwd: string,
): { action: string; payload: Record<string, string> } | null {
  if (/^https?:\/\//i.test(href))
    return { action: "openURL", payload: { url: href } };
  if (/^[a-z][a-z0-9+.-]*:/i.test(href) && !href.startsWith("file:"))
    return null;
  if (href.startsWith("#")) return null;
  let path = href.startsWith("file:")
    ? href.replace(/^file:\/\/(localhost)?/, "")
    : href;
  try {
    path = decodeURIComponent(path);
  } catch {
    return null;
  }
  return {
    action: "openFile",
    payload: { path: path.startsWith("/") ? path : cwd + "/" + path },
  };
}
