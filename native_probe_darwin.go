//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
static const char *popchatProbeDelegateClass(void) { return NSStringFromClass([[NSApp delegate] class]).UTF8String; }
static int popchatProbeReopenSupported(void) { return [[NSApp delegate] respondsToSelector:@selector(applicationShouldHandleReopen:hasVisibleWindows:)]; }
static void popchatProbeDockReopen(void) {
 [(id<NSApplicationDelegate>)[NSApp delegate] applicationShouldHandleReopen:NSApp hasVisibleWindows:NO];
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"pi-popchat/internal/agent"
)

var nativeProbeReopenCount atomic.Int64

type nativeProbeTraceEntry struct {
	MS     int64  `json:"ms"`
	Window string `json:"window"`
	Event  string `json:"event"`
}

var nativeProbeTrace struct {
	sync.Mutex
	start      time.Time
	entries    []nativeProbeTraceEntry
	ready      map[string]bool
	navigation map[string]bool
}

// Register before app.Run, otherwise the readiness events may already have fired.
func registerNativeProbeEvents(d *Desktop) {
	nativeProbeTrace.Lock()
	nativeProbeTrace.start = time.Now()
	nativeProbeTrace.ready = map[string]bool{}
	nativeProbeTrace.navigation = map[string]bool{}
	nativeProbeTrace.entries = nil
	nativeProbeTrace.Unlock()
	for name, w := range map[string]*application.WebviewWindow{"main": d.main, "panel": d.panel} {
		for event, label := range map[events.WindowEventType]string{
			events.Common.WindowRuntimeReady:      "runtime-ready",
			events.Mac.WebViewDidFinishNavigation: "navigation-finished",
			events.Common.WindowShow:              "show",
			events.Mac.WindowDidOrderOnScreen:     "ordered-on",
			events.Mac.WindowDidOrderOffScreen:    "ordered-off",
			events.Mac.WindowDidBecomeKey:         "became-key",
			events.Common.WindowClosing:           "closing",
		} {
			w.RegisterHook(event, func(*application.WindowEvent) {
				nativeProbeTrace.Lock()
				defer nativeProbeTrace.Unlock()
				nativeProbeTrace.entries = append(nativeProbeTrace.entries, nativeProbeTraceEntry{time.Since(nativeProbeTrace.start).Milliseconds(), name, label})
				if label == "runtime-ready" {
					nativeProbeTrace.ready[name] = true
				}
				if label == "navigation-finished" {
					nativeProbeTrace.navigation[name] = true
				}
			})
		}
	}
	// Simulate closing during initial load, before either WebView has navigated.
	// This must precede app.Run; readiness waits below verify late navigation
	// does not resurrect either explicitly hidden window.
	d.mu.Lock()
	d.mainWanted = false
	d.panelWanted = false
	d.mu.Unlock()
	nativeProbeMark("hide-intent-before-first-navigation")
}
func nativeProbeMark(label string) {
	nativeProbeTrace.Lock()
	defer nativeProbeTrace.Unlock()
	nativeProbeTrace.entries = append(nativeProbeTrace.entries, nativeProbeTraceEntry{time.Since(nativeProbeTrace.start).Milliseconds(), "probe", label})
}

// This owned-window harness is entered only by PI_POPCHAT_CHECK_DESKTOP=1.
// The caller must skip normal startup workers, notifications and shortcuts.
// It returns an exit code; call app.Quit afterwards and exit after app.Run.
func runDesktopProbe(d *Desktop) int {
	type result struct {
		Name   string `json:"name"`
		Pass   bool   `json:"pass"`
		Detail string `json:"detail,omitempty"`
	}
	results := []result{}
	record := func(name string, pass bool, detail string) { results = append(results, result{name, pass, detail}) }
	wait := func(check func() bool) bool {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return true
			}
			time.Sleep(40 * time.Millisecond)
		}
		return check()
	}
	ready := wait(func() bool {
		nativeProbeTrace.Lock()
		defer nativeProbeTrace.Unlock()
		return nativeProbeTrace.ready["main"] && nativeProbeTrace.ready["panel"] && nativeProbeTrace.navigation["main"] && nativeProbeTrace.navigation["panel"]
	})
	record("both-webviews-ready", ready, "requires runtime-ready and navigation-finished for main and panel")
	record("startup-hide-before-navigation", ready && !d.main.IsVisible() && !d.panel.IsVisible(), fmt.Sprintf("mainVisible=%v panelVisible=%v", d.main.IsVisible(), d.panel.IsVisible()))
	nativeProbeMark("begin-window-checks")
	root := d.engine.Root()
	_, err := d.engine.NewSession("main", "")
	record("isolated-session", err == nil, root)
	screens := nativeScreens()
	record("multiple-native-screens", len(screens) >= 2, fmt.Sprintf("count=%d", len(screens)))
	for _, screen := range screens {
		d.hidePanel()
		d.main.SetScreen(screen)
		d.showMain()
		matched := wait(func() bool { s, ok := activeWindowSelection(); return ok && s != nil && s.ID == screen.ID })
		record("active-window-"+screen.ID, matched, screen.ID)
		d.showPanel()
		placed := wait(func() bool {
			s, e := d.panel.GetScreen()
			return e == nil && s != nil && s.ID == screen.ID && d.panel.IsVisible()
		})
		record("panel-placement-"+screen.ID, placed, screen.ID)
	}
	if len(screens) > 1 {
		d.hidePanel()
		d.main.SetScreen(screens[0])
		d.showMain()
		wait(func() bool { s, ok := activeWindowSelection(); return ok && s != nil && s.ID == screens[0].ID })
		d.showPanel()
		d.main.SetScreen(screens[1])
		d.showMain()
		wait(func() bool { s, ok := activeWindowSelection(); return ok && s != nil && s.ID == screens[1].ID })
		d.togglePanel()
		record("visible-panel-moves", wait(func() bool {
			s, e := d.panel.GetScreen()
			return e == nil && s != nil && s.ID == screens[1].ID && d.panel.IsVisible()
		}), "")
		d.togglePanel()
		record("same-screen-toggle-hides", wait(func() bool { return !d.panel.IsVisible() }), "")
	}
	d.showPanel()
	sid := d.engine.CurrentID("panel")
	_, err = d.Action("panel", "transfer", nil)
	record("transfer-same-session", err == nil && wait(func() bool { return d.main.IsVisible() && !d.panel.IsVisible() }) && d.engine.CurrentID("main") == sid, "")
	d.showPanel()
	d.panel.Close()
	record("panel-close-hides", wait(func() bool { return !d.panel.IsVisible() }), "")
	d.main.Close()
	record("main-close-hides", wait(func() bool { return !d.main.IsVisible() }), "")
	nativeProbeMark("before-dock-reopen")
	beforeReopen := nativeProbeReopenCount.Load()
	var delegateClass string
	var reopenSupported bool
	application.InvokeSync(func() {
		delegateClass = C.GoString(C.popchatProbeDelegateClass())
		reopenSupported = C.popchatProbeReopenSupported() != 0
		C.popchatProbeDockReopen()
	})
	reopenPass := wait(func() bool {
		return d.main.IsVisible() && !d.panel.IsVisible() && nativeProbeReopenCount.Load() > beforeReopen
	})
	record("dock-reopen-main-only", reopenPass, fmt.Sprintf("delegate=%s supported=%v hookDelta=%d mainVisible=%v panelVisible=%v", delegateClass, reopenSupported, nativeProbeReopenCount.Load()-beforeReopen, d.main.IsVisible(), d.panel.IsVisible()))
	nativeProbeMark("after-dock-reopen")
	var willEnter, didEnter, willExit, didExit atomic.Int64
	undoEvents := []func(){
		d.main.OnWindowEvent(events.Mac.WindowWillEnterFullScreen, func(*application.WindowEvent) { willEnter.Add(1) }),
		d.main.OnWindowEvent(events.Mac.WindowDidEnterFullScreen, func(*application.WindowEvent) { didEnter.Add(1) }),
		d.main.OnWindowEvent(events.Mac.WindowWillExitFullScreen, func(*application.WindowEvent) { willExit.Add(1) }),
		d.main.OnWindowEvent(events.Mac.WindowDidExitFullScreen, func(*application.WindowEvent) { didExit.Add(1) }),
	}
	defer func() {
		for _, undo := range undoEvents {
			undo()
		}
	}()
	fullscreenDetail := func() string {
		return fmt.Sprintf("willEnter=%d didEnter=%d willExit=%d didExit=%d fullscreen=%v mainFocused=%v panelVisible=%v", willEnter.Load(), didEnter.Load(), willExit.Load(), didExit.Load(), d.main.IsFullscreen(), d.main.IsFocused(), d.panel.IsVisible())
	}
	d.showMain()
	mainFocused := wait(func() bool { return d.main.IsFocused() })
	if mainFocused {
		d.main.Fullscreen()
	}
	fullscreen := wait(func() bool { return didEnter.Load() > 0 && d.main.IsFullscreen() })
	record("owned-main-fullscreen", fullscreen, fullscreenDetail())
	if fullscreen {
		d.showPanel()
		record("panel-in-owned-fullscreen", wait(func() bool { return d.panel.IsVisible() && d.panel.IsFocused() }), "visibility+keyboard focus, not pixel occlusion")
		d.hidePanel()
		d.showMain()
		focusRestored := wait(func() bool { return !d.panel.IsVisible() && d.main.IsFocused() })
		if focusRestored {
			d.main.UnFullscreen()
		}
		left := wait(func() bool { return didExit.Load() > 0 && !d.main.IsFullscreen() })
		record("leave-fullscreen", left, fmt.Sprintf("focusRestored=%v %s", focusRestored, fullscreenDetail()))
	}
	failed := 0
	for _, r := range results {
		if !r.Pass {
			failed++
		}
	}
	nativeProbeTrace.Lock()
	trace := append([]nativeProbeTraceEntry{}, nativeProbeTrace.entries...)
	nativeProbeTrace.Unlock()
	b, _ := json.MarshalIndent(map[string]any{"suite": "owned-native-desktop", "results": results, "failed": failed, "dataDir": root, "windowEvents": trace}, "", "  ")
	fmt.Println(string(b))
	if path := os.Getenv("PI_POPCHAT_CHECK_OUTPUT"); path != "" {
		_ = os.WriteFile(path, b, 0600)
	}
	if failed > 0 {
		return 1
	}
	return 0
}

type nativeProbeFactory struct{}
type nativeProbeClient struct {
	events chan map[string]any
	once   sync.Once
}

func (nativeProbeFactory) Start(context.Context, agent.Config) (agent.Client, error) {
	return &nativeProbeClient{events: make(chan map[string]any)}, nil
}
func (c *nativeProbeClient) Request(context.Context, map[string]any) (map[string]any, error) {
	return map[string]any{"data": map[string]any{}}, nil
}
func (c *nativeProbeClient) Events() <-chan map[string]any { return c.events }
func (c *nativeProbeClient) Close() error                  { c.once.Do(func() { close(c.events) }); return nil }
