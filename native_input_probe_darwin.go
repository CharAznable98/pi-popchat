//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework WebKit
#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
static void popchatProbeTypeReady(void *pointer, bool panel, int attempt) {
 NSWindow *window = (__bridge NSWindow *)pointer;
 WKWebView *webView = [window valueForKey:@"webView"];
 NSString *characters = panel ? @"p" : @"m";
 unsigned short keyCode = panel ? 35 : 46;
 [webView evaluateJavaScript:@"(() => { const t = document.querySelector('textarea'); t.focus(); return document.hasFocus() && document.activeElement === t; })()" completionHandler:^(id result, NSError *error) {
  if (error) return;
  if (![result boolValue]) {
   if (attempt < 40) dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 50 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{ popchatProbeTypeReady(pointer, panel, attempt + 1); });
   return;
  }
  NSEvent *event = [NSEvent keyEventWithType:NSEventTypeKeyDown location:NSZeroPoint modifierFlags:0
    timestamp:NSProcessInfo.processInfo.systemUptime windowNumber:window.windowNumber context:nil
    characters:characters charactersIgnoringModifiers:characters isARepeat:NO keyCode:keyCode];
  [NSApp sendEvent:event];
 }];
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

func probeKeyboardInput(window *application.WebviewWindow, panel bool) {
	application.InvokeSync(func() {
		C.popchatProbeTypeReady(window.NativeWindow(), C.bool(panel), 0)
	})
}
