//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
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
    styleMask:NSWindowStyleMaskTitled backing:NSBackingStoreBuffered defer:NO];
  window.title = @"Popchat isolated screen fixture";
  [app finishLaunching];
  [window setFrameOrigin:frame.origin];
  [window makeKeyAndOrderFront:nil];
  [app activateIgnoringOtherApps:YES];
  [app run];
 }
}
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
func frontmostPID() int {
	var pid int
	application.InvokeSync(func() { pid = int(C.popchatFrontmostPID()) })
	return pid
}
