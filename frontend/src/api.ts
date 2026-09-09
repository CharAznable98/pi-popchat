import { Events } from '@wailsio/runtime';
import * as Desktop from '../bindings/pi-popchat/desktop';
import type { Attachment, Snapshot } from './types';
export const view = new URLSearchParams(location.search).get('view') === 'panel' ? 'panel' : 'main';
export const api = {
  snapshot: () => Desktop.Snapshot(view) as Promise<Snapshot>,
  action: (action: string, payload: Record<string, unknown> = {}) => Desktop.Action(view, action, payload) as Promise<Snapshot>,
  save: (name: string, mime: string, base64: string) => Desktop.SaveAttachment(name, mime, base64) as Promise<Attachment>,
  subscribe: (fn: () => void) => Events.On('popchat:changed', fn),
};
