package core

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type Attachment struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	MIME    string `json:"mime"`
	Preview string `json:"preview,omitempty"`
}
type Message struct {
	AgentKey    string       `json:"agentKey,omitempty"`
	ID          string       `json:"id"`
	Role        string       `json:"role"`
	Text        string       `json:"text"`
	Status      string       `json:"status"`
	CreatedAt   string       `json:"createdAt"`
	Attachments []Attachment `json:"attachments"`
}
type Interaction struct {
	ID           string   `json:"id"`
	Method       string   `json:"method"`
	Title        string   `json:"title"`
	Options      []string `json:"options"`
	Message      string   `json:"message"`
	DefaultValue string   `json:"defaultValue"`
}
type Model struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}
type Command struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}
type Session struct {
	DraftRevision    uint64       `json:"draftRevision"`
	DraftAttachments []Attachment `json:"draftAttachments"`
	ID               string       `json:"id"`
	Title            string       `json:"title"`
	Pinned           bool         `json:"pinned"`
	CWD              string       `json:"cwd"`
	CreatedAt        string       `json:"createdAt"`
	UpdatedAt        string       `json:"updatedAt"`
	Status           string       `json:"status"`
	Error            string       `json:"error"`
	Draft            string       `json:"draft"`
	Messages         []Message    `json:"messages"`
	Queue            []Message    `json:"queue"`
	QueuePaused      bool         `json:"queuePaused"`
	Interaction      *Interaction `json:"interaction"`
	ModelsState      string       `json:"modelsState"`
	ModelsError      string       `json:"modelsError"`
	Models           []Model      `json:"models"`
	Model            string       `json:"model"`
	Commands         []Command    `json:"commands"`
	SearchableText   string       `json:"searchableText"`
	SessionFile      string       `json:"sessionFile"`
	Provider         string       `json:"provider"`
}
type Settings struct {
	Shortcut string `json:"shortcut"`
	PiPath   string `json:"piPath"`
}
type Environment struct {
	PiPath    string `json:"piPath"`
	Version   string `json:"version"`
	Available bool   `json:"available"`
	Error     string `json:"error"`
}
type Snapshot struct {
	Version     uint64      `json:"version"`
	CurrentID   string      `json:"currentId"`
	Sessions    []*Session  `json:"sessions"`
	Current     *Session    `json:"current"`
	Settings    Settings    `json:"settings"`
	Environment Environment `json:"environment"`
	Error       string      `json:"error"`
}

func id() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func busy(s string) bool {
	return s == "starting" || s == "running" || s == "retrying" || s == "waiting"
}
func normalize(s *Session) {
	if s.DraftAttachments == nil {
		s.DraftAttachments = []Attachment{}
	}
	if s.Messages == nil {
		s.Messages = []Message{}
	}
	if s.Queue == nil {
		s.Queue = []Message{}
	}
	if s.Models == nil {
		s.Models = []Model{}
	}
	if s.Commands == nil {
		s.Commands = []Command{}
	}
}
