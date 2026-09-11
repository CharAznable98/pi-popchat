export type Attachment = {
  id: string;
  name: string;
  path: string;
  mime: string;
  preview?: string;
};
export type Message = {
  steps?: ProcessStep[];
  id: string;
  role: string;
  text: string;
  status: string;
  createdAt: string;
  attachments: Attachment[];
};
export type Interaction = {
  id: string;
  method: string;
  title: string;
  options?: string[];
  message?: string;
  defaultValue?: string;
};
export type Session = {
  titleWritable?: boolean;
  draftOnly?: boolean;
  managedWorkspace?: boolean;
  id: string;
  title: string;
  pinned: boolean;
  cwd: string;
  createdAt: string;
  updatedAt: string;
  status: string;
  error: string;
  draft: string;
  draftRevision?: number;
  draftAttachments?: Attachment[];
  queuePaused?: boolean;
  messages: Message[];
  queue: Message[];
  interaction: Interaction | null;
  modelsState?: "" | "loading" | "ready" | "error";
  modelsError?: string;
  models: { id: string; name: string; provider: string }[];
  model: string;
  provider?: string;
  commands: { name: string; description: string; source: string }[];
  searchableText?: string;
};
export type SelectionConfig = {
  enabled: boolean;
  buttons: { id: string; name: string; template: string }[];
};
export type Snapshot = {
  selectionPermission?: boolean;
  version: number;
  currentId: string;
  sessions: Session[];
  current: Session | null;
  settings: { shortcut: string; piPath: string; selection?: SelectionConfig };
  environment: {
    piPath: string;
    version: string;
    available: boolean;
    error: string;
  };
  error: string;
};

export type UIAction = (
  action: string,
  payload?: Record<string, unknown>,
) => void;

export type ProcessStep = {
  id: string;
  action: string;
  object?: string;
  status: string;
  startedAt: string;
  endedAt?: string;
};
