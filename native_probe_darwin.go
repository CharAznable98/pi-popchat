//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
static void popchatProbeDeactivate(void) {
 NSRunningApplication *finder = [[NSRunningApplication runningApplicationsWithBundleIdentifier:@"com.apple.finder"] firstObject];
 [finder activateWithOptions:0];
}
static bool popchatProbeIsActive(void) { return NSApp.isActive; }
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
	"os/exec"
	"path/filepath"
	"strings"
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
	defer preserveProbeCursor()()
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
	// The foreground window deliberately stays on another display. Exercise
	// the same callback registered with GlobalShortcut, including activation.
	for i, target := range screens {
		d.hidePanel()
		d.main.SetScreen(screens[(i+1)%len(screens)])
		d.showMain()
		wait(func() bool { return d.main.IsFocused() })
		for _, point := range [][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {0.5, 0.5}} {
			moved := moveProbeCursor(target, point[0], point[1])
			selected := wait(func() bool { s := mouseScreen(); return s != nil && s.ID == target.ID })
			record(fmt.Sprintf("mouse-select-%s-%.1f-%.1f", target.ID, point[0], point[1]), moved && selected, "full display frame, including edges; AppKit points")
		}
		d.togglePanel()
		record("mouse-panel-centered-"+target.ID, wait(func() bool {
			s, err := d.panel.GetScreen()
			return err == nil && s != nil && s.ID == target.ID && d.panel.IsVisible() && d.panel.IsFocused() && panelCentered(d.panel)
		}), "foreground main window is on another screen when multiple screens are available")
		if len(screens) > 1 {
			other := screens[(i+1)%len(screens)]
			moveProbeCursor(other, 0.5, 0.5)
			wait(func() bool { s := mouseScreen(); return s != nil && s.ID == other.ID })
			time.Sleep(150 * time.Millisecond)
			s, _ := d.panel.GetScreen()
			record("mouse-motion-does-not-move-panel-"+target.ID, s != nil && s.ID == target.ID && d.panel.IsVisible(), "cursor motion alone must not move the panel")
			d.togglePanel()
			record("mouse-cross-screen-toggle-"+target.ID, wait(func() bool {
				s, err := d.panel.GetScreen()
				return err == nil && s != nil && s.ID == other.ID && d.panel.IsVisible() && d.panel.IsFocused() && panelCentered(d.panel)
			}), "visible panel follows the next explicit shortcut invocation")
		}
		d.togglePanel()
		record("mouse-same-screen-hides-"+target.ID, wait(func() bool { return !d.panel.IsVisible() }), "")
	}
	for screenIndex, screen := range screens {
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
		if screenIndex == 0 {
			d.hidePanel()
			d.showMain()
			probeKeyboardInput(d.main, false)
			record("main-keyboard-input", wait(func() bool {
				s := d.engine.Snapshot("main").Current
				return s != nil && strings.EqualFold(s.Draft, "m")
			}), fmt.Sprintf("AppKit keyDown → draft: %q", d.engine.Snapshot("main").Current.Draft))
			d.hideMain()
			application.InvokeSync(func() { C.popchatProbeDeactivate() })
			inactive := wait(func() bool {
				var active bool
				application.InvokeSync(func() { active = bool(C.popchatProbeIsActive()) })
				return !active
			})
			d.showPanel()
			activated := wait(func() bool {
				var active bool
				application.InvokeSync(func() { active = bool(C.popchatProbeIsActive()) })
				return active && d.panel.IsFocused()
			})
			record("explicit-panel-activation", inactive && activated, fmt.Sprintf("inactiveBefore=%v activeAndKeyAfter=%v panelFocused=%v mainFocused=%v", inactive, activated, d.panel.IsFocused(), d.main.IsFocused()))
			probeKeyboardInput(d.panel, true)
			record("panel-keyboard-input", wait(func() bool {
				s := d.engine.Snapshot("panel").Current
				return s != nil && strings.EqualFold(s.Draft, "p")
			}), "AppKit keyDown must reach React and persisted draft")
		}

	}
	if len(screens) > 1 {
		for _, target := range screens[1:] {
			d.hidePanel()
			d.main.SetScreen(screens[0])
			d.showMain()
			binary, _ := os.Executable()
			fixturePath := filepath.Join(root, "screen-fixture")
			bytes, copyErr := os.ReadFile(binary)
			if copyErr == nil {
				copyErr = os.WriteFile(fixturePath, bytes, 0700)
			}
			if copyErr != nil {
				record("foreign-window-start-"+target.ID, false, copyErr.Error())
				continue
			}
			// Outside the product .app bundle: the fixture must be a distinct
			// macOS application, not another process with our bundle identity.
			fixture := exec.Command(fixturePath)
			fixture.Env = append(os.Environ(), "PI_POPCHAT_SCREEN_FIXTURE="+target.ID)
			err := fixture.Start()
			if err != nil {
				record("foreign-window-start-"+target.ID, false, err.Error())
				continue
			}
			foreground := wait(func() bool { return frontmostPID() == fixture.Process.Pid })
			wait(func() bool {
				_, matched := activeWindowSelection()
				return frontmostPID() == fixture.Process.Pid && matched
			})
			// A newly launched AppKit process can still complete activation after
			// first appearing in the window server. Require a settled foreground
			// before simulating a user invoking the shortcut from that application.
			stableSince := time.Now()
			wait(func() bool {
				s, ok := activeWindowSelection()
				if frontmostPID() != fixture.Process.Pid || !ok || s == nil || s.ID != target.ID {
					stableSince = time.Now()
					return false
				}
				return time.Since(stableSince) >= 500*time.Millisecond
			})
			selected, matched := activeWindowSelection()
			selectedID := "none"
			if selected != nil {
				selectedID = selected.ID
			}
			record("foreign-active-window-"+target.ID, foreground && matched && selectedID == target.ID, fmt.Sprintf("expected=%s actual=%s main=%s foreignForeground=%v matched=%v frontPID=%d fixturePID=%d", target.ID, selectedID, screens[0].ID, foreground, matched, frontmostPID(), fixture.Process.Pid))
			moveProbeCursor(target, 0.5, 0.5)
			wait(func() bool { s := mouseScreen(); return s != nil && s.ID == target.ID })
			d.togglePanel()
			placed := wait(func() bool {
				screen, err := d.panel.GetScreen()
				return err == nil && screen != nil && screen.ID == target.ID && d.panel.IsVisible() && d.panel.IsFocused()
			})
			panelScreen, _ := d.panel.GetScreen()
			panelScreenID := "none"
			if panelScreen != nil {
				panelScreenID = panelScreen.ID
			}
			record("foreign-window-panel-placement-"+target.ID, placed, fmt.Sprintf("expected=%s actual=%s visible=%v panelKey=%v mainKey=%v frontPID=%d ownPID=%d", target.ID, panelScreenID, d.panel.IsVisible(), d.panel.IsFocused(), d.main.IsFocused(), frontmostPID(), os.Getpid()))
			_ = fixture.Process.Kill()
			_ = fixture.Wait()
			d.showMain()
			wait(func() bool { return frontmostPID() == os.Getpid() && d.main.IsFocused() })
		}
	}
	if len(screens) > 1 {
		d.hidePanel()
		d.main.SetScreen(screens[0])
		d.showMain()
		d.showPanel()
		d.panel.SetScreen(screens[1])
		d.panel.Focus()
		keyPanel := wait(func() bool { return d.panel.IsFocused() })
		selected, matched := activeWindowSelection()
		selectedID := "none"
		if selected != nil {
			selectedID = selected.ID
		}
		record("active-panel-screen-differs-from-main", keyPanel && matched && selectedID == screens[1].ID, fmt.Sprintf("expected=%s actual=%s main=%s", screens[1].ID, selectedID, screens[0].ID))
		moveProbeCursor(screens[1], 0.5, 0.5)
		wait(func() bool { s := mouseScreen(); return s != nil && s.ID == screens[1].ID })
		d.togglePanel()
		record("same-screen-toggle-with-main-on-other-display", wait(func() bool { return !d.panel.IsVisible() }), "key panel must hide rather than jump to main window display")
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
		moveProbeCursor(screens[1], 0.5, 0.5)
		wait(func() bool { s := mouseScreen(); return s != nil && s.ID == screens[1].ID })
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
	// A foreign fullscreen Space is distinct from our own fullscreen window.
	for _, target := range screens {
		d.hidePanel()
		d.main.SetScreen(screens[0])
		d.showMain()
		binary, _ := os.Executable()
		fixturePath := filepath.Join(root, "fullscreen-fixture")
		data, copyErr := os.ReadFile(binary)
		if copyErr == nil {
			copyErr = os.WriteFile(fixturePath, data, 0700)
		}
		readyPath := filepath.Join(root, "fullscreen-ready-"+target.ID)
		fixture := exec.Command(fixturePath)
		fixture.Env = append(os.Environ(), "PI_POPCHAT_SCREEN_FIXTURE="+target.ID, "PI_POPCHAT_FIXTURE_FULLSCREEN=1", "PI_POPCHAT_FIXTURE_READY="+readyPath)
		if copyErr == nil {
			copyErr = fixture.Start()
		}
		record("foreign-fullscreen-start-"+target.ID, copyErr == nil, fmt.Sprint(copyErr))
		if copyErr == nil {
			ready := wait(func() bool {
				_, err := os.Stat(readyPath)
				return err == nil && frontmostPID() == fixture.Process.Pid && fixtureOnScreen(fixture.Process.Pid)
			})
			record("foreign-fullscreen-ready-"+target.ID, ready, "separate AppKit process completed native fullscreen transition")
			if ready {
				moveProbeCursor(target, 0.5, 0.5)
				wait(func() bool { s := mouseScreen(); return s != nil && s.ID == target.ID })
				selected := mouseScreen()
				selectedID := "none"
				if selected != nil {
					selectedID = selected.ID
				}
				d.togglePanel()
				// Let application activation and any Space transition settle before asserting.
				time.Sleep(1200 * time.Millisecond)
				pass := wait(func() bool {
					screen, err := d.panel.GetScreen()
					return err == nil && screen != nil && screen.ID == target.ID && d.panel.IsFocused() && panelOnActiveSpace(d.panel) && panelAboveFixture(d.panel, fixture.Process.Pid)
				})
				actual, _ := d.panel.GetScreen()
				actualID := "none"
				if actual != nil {
					actualID = actual.ID
				}
				record("panel-over-foreign-fullscreen-"+target.ID, pass, fmt.Sprintf("target=%s selected=%s actual=%s panelFocused=%v panelActiveSpace=%v fullscreenStillOnScreen=%v frontPID=%d ownPID=%d fixturePID=%d %s", target.ID, selectedID, actualID, d.panel.IsFocused(), panelOnActiveSpace(d.panel), fixtureOnScreen(fixture.Process.Pid), frontmostPID(), os.Getpid(), fixture.Process.Pid, panelSpaceDetail(d.panel, fixture.Process.Pid)))
			}
			d.hidePanel()
			_ = fixture.Process.Kill()
			_ = fixture.Wait()
			d.showMain()
			wait(func() bool { return d.main.IsFocused() && panelOnActiveSpace(d.main) })
			// Destruction of the foreign fullscreen Space has its own animation.
			time.Sleep(1200 * time.Millisecond)
		}
	}
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
		record("panel-in-owned-fullscreen", wait(func() bool { return d.panel.IsVisible() && d.panel.IsFocused() }), fmt.Sprintf("visible=%v focused=%v frontPID=%d ownPID=%d; not pixel occlusion", d.panel.IsVisible(), d.panel.IsFocused(), frontmostPID(), os.Getpid()))
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
	done   chan struct{}
	events chan map[string]any
	once   sync.Once
}

func (nativeProbeFactory) Start(context.Context, agent.Config) (agent.Client, error) {
	return &nativeProbeClient{events: make(chan map[string]any), done: make(chan struct{})}, nil
}
func (c *nativeProbeClient) Request(context.Context, map[string]any) (map[string]any, error) {
	return map[string]any{"data": map[string]any{}}, nil
}
func (c *nativeProbeClient) Events() <-chan map[string]any { return c.events }
func (c *nativeProbeClient) Done() <-chan struct{}         { return c.done }
func (c *nativeProbeClient) Close() error {
	c.once.Do(func() { close(c.done); close(c.events) })
	return nil
}
