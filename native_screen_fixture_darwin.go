//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>
static CGPoint popchatProbeCursor(void) {
 CGEventRef event = CGEventCreate(NULL);
 CGPoint point = CGEventGetLocation(event);
 CFRelease(event);
 return point;
}
static void popchatProbeRestoreCursor(CGPoint point) { CGWarpMouseCursorPosition(point); }
static bool popchatProbeMoveCursor(unsigned int display, double x, double y) {
 for (NSScreen *screen in NSScreen.screens) {
  if ([screen.deviceDescription[@"NSScreenNumber"] unsignedIntValue] != display) continue;
  NSRect frame = screen.frame;
  CGFloat top = NSMaxY(NSScreen.screens.firstObject.frame);
  return CGWarpMouseCursorPosition(CGPointMake(NSMinX(frame) + x * (frame.size.width - 1),
    top - (NSMinY(frame) + y * (frame.size.height - 1)))) == kCGErrorSuccess;
 }
 return false;
}
static bool popchatProbeCentered(void *pointer) {
 NSWindow *window = (__bridge NSWindow *)pointer;
 NSRect frame = window.frame, area = window.screen.visibleFrame;
 return fabs(NSMidX(frame) - NSMidX(area)) <= 2 && fabs(NSMidY(frame) - NSMidY(area)) <= 2;
}
static void popchatScreenFixture(unsigned int display) {
 @autoreleasepool {
  NSApplication *app = [NSApplication sharedApplication];
  [app setActivationPolicy:NSApplicationActivationPolicyRegular];
  NSScreen *target = nil;
  for (NSScreen *screen in NSScreen.screens) {
   if ([screen.deviceDescription[@"NSScreenNumber"] unsignedIntValue] == display) target = screen;
  }
  if (!target) return;
  NSRect area = target.visibleFrame;
  NSRect frame = NSMakeRect(NSMidX(area)-260, NSMidY(area)-180, 520, 360);
  NSWindow *window = [[NSWindow alloc] initWithContentRect:frame
    styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskResizable) backing:NSBackingStoreBuffered defer:NO];
  window.title = @"Popchat isolated screen fixture";
  [app finishLaunching];
  [window setFrameOrigin:frame.origin];
  [window makeKeyAndOrderFront:nil];
  [app activateIgnoringOtherApps:YES];
  if (getenv("PI_POPCHAT_FIXTURE_FULLSCREEN")) {
   window.collectionBehavior = NSWindowCollectionBehaviorFullScreenPrimary;
   [[NSNotificationCenter defaultCenter] addObserverForName:NSWindowDidEnterFullScreenNotification object:window queue:nil usingBlock:^(NSNotification *note) {
    NSString *path = NSProcessInfo.processInfo.environment[@"PI_POPCHAT_FIXTURE_READY"];
    [@"ready" writeToFile:path atomically:YES encoding:NSUTF8StringEncoding error:nil];
   }];
   dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 500 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{ [window toggleFullScreen:nil]; });
  }
  [app run];
 }
}
static bool popchatFixtureOnScreen(int pid) {
 CFArrayRef list = CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly, kCGNullWindowID);
 bool found = false;
 for (NSDictionary *w in (__bridge NSArray *)list) {
  if ([w[(__bridge NSString *)kCGWindowOwnerPID] intValue] == pid && [w[(__bridge NSString *)kCGWindowLayer] intValue] == 0) found = true;
 }
 if (list) CFRelease(list);
 return found;
}
static const char *popchatPanelSpaceDetail(void *pointer, int pid) {
 NSWindow *p=(__bridge NSWindow *)pointer;
 CFArrayRef list=CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly,kCGNullWindowID);
 int panelIndex=-1,foreignIndex=-1,index=0;
 for (NSDictionary *w in (__bridge NSArray *)list) {
  if ([w[(__bridge NSString *)kCGWindowNumber] intValue] == p.windowNumber) panelIndex=index;
  if ([w[(__bridge NSString *)kCGWindowOwnerPID] intValue]==pid && [w[(__bridge NSString *)kCGWindowLayer] intValue]==0 && foreignIndex<0) foreignIndex=index;
  index++;
 }
 if(list)CFRelease(list);
 return [NSString stringWithFormat:@"behavior=%lu level=%ld style=%lu panelIndex=%d foreignIndex=%d occlusion=%lu",(unsigned long)p.collectionBehavior,(long)p.level,(unsigned long)p.styleMask,panelIndex,foreignIndex,(unsigned long)p.occlusionState].UTF8String;
}
static bool popchatPanelAboveFixture(void *pointer, int pid) {
 NSWindow *panel=(__bridge NSWindow *)pointer;
 CFArrayRef list=CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly,kCGNullWindowID);
 bool panelSeen=false, above=false;
 for (NSDictionary *w in (__bridge NSArray *)list) {
  if ([w[(__bridge NSString *)kCGWindowNumber] intValue]==panel.windowNumber) panelSeen=true;
  if ([w[(__bridge NSString *)kCGWindowOwnerPID] intValue]==pid && [w[(__bridge NSString *)kCGWindowLayer] intValue]==0) { above=panelSeen; break; }
 }
 if(list)CFRelease(list);
 return above;
}
static bool popchatWindowOnActiveSpace(void *pointer) { return [(__bridge NSWindow *)pointer isOnActiveSpace]; }
static int popchatFrontmostPID(void) { return NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier; }
*/
import "C"

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"strconv"
)

// The probe launches a separate process with one ordinary window. It opens no
// user data or Agent processes and is terminated by the parent after each check.
func runScreenFixture(display string) {
	id, err := strconv.ParseUint(display, 10, 32)
	if err == nil {
		C.popchatScreenFixture(C.uint(id))
	}
}

// Only used by the isolated desktop probe; restore the user's cursor on exit.
func preserveProbeCursor() func() {
	var point C.CGPoint
	application.InvokeSync(func() { point = C.popchatProbeCursor() })
	return func() { application.InvokeSync(func() { C.popchatProbeRestoreCursor(point) }) }
}

func moveProbeCursor(screen *application.Screen, x, y float64) bool {
	id, err := strconv.ParseUint(screen.ID, 10, 32)
	if err != nil {
		return false
	}
	var ok bool
	application.InvokeSync(func() { ok = bool(C.popchatProbeMoveCursor(C.uint(id), C.double(x), C.double(y))) })
	return ok
}

func panelCentered(w *application.WebviewWindow) bool {
	var ok bool
	application.InvokeSync(func() { ok = bool(C.popchatProbeCentered(w.NativeWindow())) })
	return ok
}
func frontmostPID() int {
	var pid int
	application.InvokeSync(func() { pid = int(C.popchatFrontmostPID()) })
	return pid
}

func panelOnActiveSpace(w *application.WebviewWindow) bool {
	var result bool
	application.InvokeSync(func() { result = bool(C.popchatWindowOnActiveSpace(w.NativeWindow())) })
	return result
}
func fixtureOnScreen(pid int) bool { return bool(C.popchatFixtureOnScreen(C.int(pid))) }

func panelSpaceDetail(w *application.WebviewWindow, pid int) string {
	var result string
	application.InvokeSync(func() { result = C.GoString(C.popchatPanelSpaceDetail(w.NativeWindow(), C.int(pid))) })
	return result
}

func panelAboveFixture(w *application.WebviewWindow, pid int) bool {
	var result bool
	application.InvokeSync(func() { result = bool(C.popchatPanelAboveFixture(w.NativeWindow(), C.int(pid))) })
	return result
}
