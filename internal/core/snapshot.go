package core

import (
	"slices"
	"sort"
)

// Snapshot copies only the selected conversation in full. History summaries
// never traverse inactive messages, attachments, or Agent metadata.
func (e *Engine) Snapshot(view string) Snapshot {
	e.mu.Lock()
	v := Snapshot{Version: e.version, CurrentID: e.selected[view], Sessions: []*Session{}, Settings: cloneSettings(e.settings), Environment: e.environment, Error: e.lastError}
	for _, s := range e.sessions {
		if s.ID == v.CurrentID {
			v.Current = cloneSession(s)
			v.Current.ManagedWorkspace = e.managedWorkspaceLocked(s)
		}
		if s.DraftOnly {
			continue
		}
		summary := *s
		summary.ManagedWorkspace = e.managedWorkspaceLocked(s)
		summary.Messages = []Message{}
		summary.Queue = []Message{}
		summary.Models = []Model{}
		summary.Commands = []Command{}
		summary.Draft = ""
		summary.DraftAttachments = []Attachment{}
		summary.Interaction = nil
		v.Sessions = append(v.Sessions, &summary)
	}
	e.mu.Unlock()
	// The result owns its mutable data; ordering does not need the engine lock.
	sort.Slice(v.Sessions, func(i, j int) bool {
		a, b := v.Sessions[i], v.Sessions[j]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		return a.UpdatedAt > b.UpdatedAt
	})
	return v
}

func cloneSession(s *Session) *Session {
	cp := *s
	cp.Messages = cloneMessages(s.Messages)
	cp.Queue = cloneMessages(s.Queue)
	cp.Models = slices.Clone(s.Models)
	cp.Commands = slices.Clone(s.Commands)
	cp.DraftAttachments = slices.Clone(s.DraftAttachments)
	if s.Interaction != nil {
		i := *s.Interaction
		i.Options = slices.Clone(i.Options)
		cp.Interaction = &i
	}
	return &cp
}

func cloneMessages(messages []Message) []Message {
	result := slices.Clone(messages)
	for i := range result {
		result[i].Attachments = slices.Clone(messages[i].Attachments)
		result[i].Steps = slices.Clone(messages[i].Steps)
	}
	return result
}
