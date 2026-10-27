package core

import (
	"encoding/base64"
	"testing"
)

func TestSubmissionReceiptDistinguishesCommitFromDeduplication(t *testing.T) {
	e, _ := acceptanceEngine(t)
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Draft(sid, "original", nil); err != nil {
		t.Fatal(err)
	}
	revision := uint64(1)
	first, err := e.SendWithReceipt(sid, "original", "stable-message", nil, &revision)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID != sid || first.Duplicate || first.Draft != "" || first.DraftRevision != 2 {
		t.Fatalf("incorrect first submission receipt: %+v", first)
	}
	attachment, err := e.SaveAttachment("receipt.txt", "text/plain", base64.StdEncoding.EncodeToString([]byte("synthetic")))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.DraftWithAttachments(sid, "saved retry", nil, []Attachment{attachment}); err != nil {
		t.Fatal(err)
	}
	duplicate, err := e.SendWithReceipt(sid, "original", "stable-message", nil, &revision)
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate.Duplicate || duplicate.Draft != "saved retry" || duplicate.DraftRevision != 3 || len(duplicate.DraftAttachments) != 1 {
		t.Fatalf("deduplication changed the draft or guessed its revision: %+v", duplicate)
	}
	duplicate.DraftAttachments[0].Name = "changed copy"
	if e.Snapshot("main").Current.DraftAttachments[0].Name != "receipt.txt" {
		t.Fatal("receipt shares mutable attachments")
	}
	if err = e.Draft(sid, "later draft", nil); err != nil {
		t.Fatal(err)
	}
	if duplicate.Draft != "saved retry" || duplicate.DraftRevision != 3 {
		t.Fatal("receipt changed after a later mutation")
	}
	revision = 4
	queued, err := e.SendWithReceipt(sid, "queued", "queued-message", nil, &revision)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Duplicate || queued.DraftRevision != 5 || queued.Draft != "" || len(e.Snapshot("main").Current.Queue) != 1 {
		t.Fatalf("queue submission has no committed draft receipt: %+v", queued)
	}
	revision = 99
	failed, err := e.SendWithReceipt(sid, "stale", "stale-message", nil, &revision)
	if err == nil || failed != nil {
		t.Fatal("failed mutation returned a success receipt")
	}
}
