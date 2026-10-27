//go:build darwin

package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"
)

// Run only in the owned native probe with its synthetic, temporary database.
// Hold a real SQLite writer to prove visibility does not depend on persistence.
func probePanelResponsiveness(d *Desktop, record func(string, bool, string)) {
	db, err := sql.Open("sqlite3", filepath.Join(d.engine.Root(), "popchat.db"))
	if err != nil {
		record("panel-responsiveness-fixture", false, err.Error())
		return
	}
	defer db.Close()
	conn, err := db.Conn(context.Background())
	if err != nil {
		record("panel-responsiveness-fixture", false, err.Error())
		return
	}
	defer conn.Close()
	if _, err = conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		record("panel-responsiveness-fixture", false, err.Error())
		return
	}
	released := make(chan struct{})
	time.AfterFunc(800*time.Millisecond, func() {
		_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		close(released)
	})
	start := time.Now()
	d.showPanel()
	visible := waitPanelVisibility(d, true, start.Add(500*time.Millisecond))
	showTime := time.Since(start)
	record("panel-shows-before-persistence", visible && showTime < 500*time.Millisecond, fmt.Sprintf("show=%s, SQLite writer held 800ms", showTime))
	_, sendErr := d.Action("panel", "send", map[string]any{"text": "synthetic must not send", "clientMessageId": "blocked-panel-send"})
	record("pending-panel-rejects-stale-send", sendErr != nil, "pending presentation must not target the previously displayed session")
	start = time.Now()
	d.hidePanel()
	hidden := waitPanelVisibility(d, false, start.Add(500*time.Millisecond))
	hideTime := time.Since(start)
	record("panel-hides-before-persistence", hidden && hideTime < 500*time.Millisecond, fmt.Sprintf("hide=%s", hideTime))
	d.showPanel()
	<-released
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		ready := !d.panelPending && d.panelWanted
		d.mu.Unlock()
		if ready {
			record("latest-panel-invocation-becomes-ready", d.panel.IsVisible(), "older open/hide callbacks cannot complete the latest presentation")
			d.hidePanel()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	record("latest-panel-invocation-becomes-ready", false, "latest presentation did not settle after releasing SQLite")
}

func waitPanelVisibility(d *Desktop, visible bool, deadline time.Time) bool {
	for time.Now().Before(deadline) {
		if d.panel.IsVisible() == visible {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}
