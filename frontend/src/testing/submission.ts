import type { Snapshot } from "../types";

// Normal successful submissions consume the draft. Tests for deduplication
// provide an explicit receipt instead of using this default mock response.
export function withSubmission(
  snapshot: Snapshot,
  action: string,
  payload: Record<string, unknown>,
): Snapshot {
  if (action !== "send" || snapshot.submission) return snapshot;
  return {
    ...snapshot,
    submission: {
      sessionId: String(payload.id ?? payload.sessionId),
      draft: "",
      draftRevision: Number(payload.expectedDraftRevision ?? 0) + 1,
      draftAttachments: [],
      duplicate: false,
    },
  };
}
