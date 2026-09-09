//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework WebKit
#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
static void popchatProbeType(void *pointer, bool panel) {
 NSWindow *window = (__bridge NSWindow *)pointer;
 WKWebView *webView = [window valueForKey:@"webView"];
 NSString *characters = panel ? @"p" : @"m";
 unsigned short keyCode = panel ? 35 : 46;
 [webView evaluateJavaScript:@"document.querySelector('textarea').focus()" completionHandler:^(id result, NSError *error) {
  if (error) return;
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
		C.popchatProbeType(window.NativeWindow(), C.bool(panel))
	})
}
