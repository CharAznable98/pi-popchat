package core

// ProcessStep is a display projection, never raw arguments, thinking or output.
type ProcessStep struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Object    string `json:"object,omitempty"`
	Status    string `json:"status"`
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt,omitempty"`
}

func toolDescription(name string, args map[string]any) (string, string) {
	switch name {
	case "read":
		return "读取文件", str(args["path"])
	case "write":
		return "写入文件", str(args["path"])
	case "edit":
		return "编辑文件", str(args["path"])
	case "ls":
		return "列出目录", str(args["path"])
	case "find":
		return "查找文件", str(args["pattern"])
	case "grep":
		return "搜索内容", str(args["path"])
	case "bash":
		return "执行命令", ""
	default:
		return name, ""
	}
}
func recordTool(s *Session, ev map[string]any) {
	tid := str(ev["toolCallId"])
	if tid == "" {
		return
	}
	for i := len(s.Messages) - 1; i >= 0; i-- {
		for j := range s.Messages[i].Steps {
			step := &s.Messages[i].Steps[j]
			if step.ID == tid {
				if str(ev["type"]) == "tool_execution_start" {
					step.Status = "running"
					step.StartedAt = now()
				}
				if str(ev["type"]) == "tool_execution_end" {
					step.Status = "completed"
					if failed, _ := ev["isError"].(bool); failed {
						step.Status = "failed"
					}
					step.EndedAt = now()
				}
				return
			}
		}
	}
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role == "user" {
			action, object := toolDescription(str(ev["toolName"]), obj(ev["args"]))
			step := ProcessStep{ID: tid, Action: action, Object: object, Status: "running", StartedAt: now()}
			if str(ev["type"]) == "tool_execution_pending" {
				step.Status = "pending"
				step.StartedAt = ""
			}
			if str(ev["type"]) == "tool_execution_end" {
				step.Status = "completed"
				if failed, _ := ev["isError"].(bool); failed {
					step.Status = "failed"
				}
				step.EndedAt = now()
			}
			s.Messages[i].Steps = append(s.Messages[i].Steps, step)
			return
		}
	}
}
func finishSteps(s *Session, status string) {
	for i := range s.Messages {
		for j := range s.Messages[i].Steps {
			step := &s.Messages[i].Steps[j]
			if step.Status == "running" || step.Status == "pending" {
				step.Status = status
				step.EndedAt = now()
			}
		}
	}
}
func fallbackTitle(s *Session) string {
	for _, m := range s.Messages {
		if m.Role == "user" {
			r := []rune(m.Text)
			if len(r) > 32 {
				r = r[:32]
			}
			if len(r) > 0 {
				return string(r)
			}
			return "附件对话"
		}
	}
	return "新对话"
}
