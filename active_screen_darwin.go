//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework CoreGraphics
#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>

static unsigned int displayID(NSScreen *screen) {
 return [[[screen deviceDescription] objectForKey:@"NSScreenNumber"] unsignedIntValue];
}
static int displayCount(void) { return (int)[[NSScreen screens] count]; }
static unsigned int displayAt(int i) { return displayID([[NSScreen screens] objectAtIndex:i]); }
static void popchatActivateForPanel(void) { if (!NSApp.isActive) [NSApp activateIgnoringOtherApps:YES]; }
static bool popchatApplicationActive(void) { return NSApp.isActive; }

// NSEvent and NSScreen.frame share AppKit global coordinates (in points).
// Use the full frame so the menu bar and Dock also select their display.
static unsigned int mouseDisplay(void) {
 @autoreleasepool {
  NSPoint mouse = [NSEvent mouseLocation];
  NSArray<NSScreen *> *screens = [NSScreen screens];
  for (NSScreen *screen in screens) {
   NSRect frame = screen.frame;
   if (mouse.x >= NSMinX(frame) && mouse.x < NSMaxX(frame) &&
       mouse.y >= NSMinY(frame) && mouse.y < NSMaxY(frame)) return displayID(screen);
  }
  // A display may disappear between input delivery and this lookup.
  return displayID([NSScreen mainScreen] ?: [screens firstObject]);
 }
}

// Inspect only the frontmost app's window geometry. No titles, pixels, AX API,
// or screen-recording permission is needed. Run before showing the panel.
static unsigned int activeWindowDisplay(int *matchedWindow) {
 @autoreleasepool {
  *matchedWindow = 0;
  NSArray<NSScreen *> *screens = [NSScreen screens];
  if ([screens count] == 0) return 0;
  pid_t pid = [[[NSWorkspace sharedWorkspace] frontmostApplication] processIdentifier];
  // Floating panels have a nonzero CGWindow layer. When this app is active,
  // its actual key window is authoritative, even if the main window is on
  // another display. Do not use a stale own key window for another active app.
  if (pid == NSProcessInfo.processInfo.processIdentifier) {
   NSWindow *key = NSApp.keyWindow;
   if (key.isVisible && key.screen != nil) {
    *matchedWindow = 1;
    return displayID(key.screen);
   }
  }
  CFArrayRef windowInfo = CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements, kCGNullWindowID);
  unsigned int selected = 0;
  if (windowInfo) {
   for (NSDictionary *info in (__bridge NSArray *)windowInfo) {
    if ([info[(__bridge NSString *)kCGWindowOwnerPID] intValue] != pid ||
        [info[(__bridge NSString *)kCGWindowLayer] intValue] != 0 ||
        [info[(__bridge NSString *)kCGWindowAlpha] doubleValue] <= 0) continue;
    CGRect bounds;
    if (!CGRectMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)info[(__bridge NSString *)kCGWindowBounds], &bounds)) continue;
    if (bounds.size.width <= 0 || bounds.size.height <= 0) continue;
    CGFloat top = NSMaxY([[screens firstObject] frame]);
    NSRect windowRect = NSMakeRect(bounds.origin.x, top - bounds.origin.y - bounds.size.height, bounds.size.width, bounds.size.height);
    CGFloat maxArea = 0;
    for (NSScreen *screen in screens) {
     NSRect overlap = NSIntersectionRect(windowRect, [screen frame]);
     CGFloat area = overlap.size.width * overlap.size.height;
     if (area > maxArea) { maxArea = area; selected = displayID(screen); }
    }
    break; // front-to-back order: foremost normal window of active application
   }
   CFRelease(windowInfo);
  }
  if (selected) {*matchedWindow = 1;return selected;}
  return displayID([NSScreen mainScreen] ?: [screens firstObject]);
 }
}
*/
import "C"

import (
	"strconv"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// AppKit NSWindowCollectionBehaviorCanJoinAllApplications (macOS 13+).
// Wails beta.18 does not name this flag. Unlike FullScreenAuxiliary alone,
// it marks this floating panel as eligible for other applications' fullscreen Spaces.
const macWindowCanJoinAllApplications application.MacWindowCollectionBehavior = 1 << 18

// Called on the AppKit main thread alongside the visibility mutation.
func nativeApplicationIsActive() bool { return bool(C.popchatApplicationActive()) }

// Capture once before activation; activation completion retains this screen.
func mouseScreen() *application.Screen {
	var id uint32
	application.InvokeSync(func() { id = uint32(C.mouseDisplay()) })
	if id == 0 {
		return nil
	}
	return &application.Screen{ID: strconv.FormatUint(uint64(id), 10)}
}

func activeWindowScreen() *application.Screen {
	screen, _ := activeWindowSelection()
	return screen
}

func activeWindowSelection() (*application.Screen, bool) {
	var id uint32
	var matched C.int
	application.InvokeSync(func() { id = uint32(C.activeWindowDisplay(&matched)) })
	if id == 0 {
		return nil, false
	}
	return &application.Screen{ID: strconv.FormatUint(uint64(id), 10)}, matched != 0
}

func nativeScreens() []*application.Screen {
	var screens []*application.Screen
	application.InvokeSync(func() {
		for i := 0; i < int(C.displayCount()); i++ {
			id := strconv.FormatUint(uint64(C.displayAt(C.int(i))), 10)
			screens = append(screens, &application.Screen{ID: id, Name: "display-" + id})
		}
	})
	return screens
}

// Explicit user invocation activates keyboard delivery while retaining the panel Space semantics.
func nativeActivateForPanel() { C.popchatActivateForPanel() }
