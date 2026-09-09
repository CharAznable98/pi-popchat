//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework WebKit
#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
static NSArray *popchatSavedClipboard;
static NSInteger popchatProbeClipboardCount;
static void popchatSaveClipboard(void) {
 NSMutableArray *saved=[NSMutableArray array];
 for (NSPasteboardItem *item in NSPasteboard.generalPasteboard.pasteboardItems) {
  NSPasteboardItem *copy=[[[NSPasteboardItem alloc] init] autorelease];
  for (NSPasteboardType type in item.types) { NSData *data=[item dataForType:type]; if(data)[copy setData:data forType:type]; }
  [saved addObject:copy];
 }
 popchatSavedClipboard=[saved copy];
 popchatProbeClipboardCount=NSPasteboard.generalPasteboard.changeCount;
}
static bool popchatPrepareClipboard(int kind) {
 NSPasteboard *pb=NSPasteboard.generalPasteboard;
 if(pb.changeCount!=popchatProbeClipboardCount)return false;
 [pb clearContents];
 if(kind==0) [pb setString:@"clipboard-text-proof" forType:NSPasteboardTypeString];
 else {
  NSBitmapImageRep *bitmap=[[[NSBitmapImageRep alloc] initWithBitmapDataPlanes:NULL pixelsWide:2 pixelsHigh:2 bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO colorSpaceName:NSDeviceRGBColorSpace bytesPerRow:8 bitsPerPixel:32] autorelease];
  memset(bitmap.bitmapData,255,16);
  NSData *data=kind==1?[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}]:bitmap.TIFFRepresentation;
  [pb setData:data forType:kind==1?NSPasteboardTypePNG:NSPasteboardTypeTIFF];
 }
 popchatProbeClipboardCount=pb.changeCount;
 return true;
}
static void popchatRestoreClipboard(void) {
 NSPasteboard *pb=NSPasteboard.generalPasteboard;
 if(pb.changeCount==popchatProbeClipboardCount) { [pb clearContents]; if(popchatSavedClipboard.count)[pb writeObjects:popchatSavedClipboard]; }
 [popchatSavedClipboard release];popchatSavedClipboard=nil;
}
static void popchatProbePasteReady(void *pointer, NSString *marker, int attempt) {
 NSWindow *window=(__bridge NSWindow *)pointer;
 WKWebView *webView=[window valueForKey:@"webView"];
 NSString *script=[NSString stringWithFormat:@"(() => { const t=document.querySelector('textarea'); if(!t || !document.body.innerText.includes('%@')) return false; t.focus(); return document.hasFocus() && document.activeElement===t; })()",marker];
 [webView evaluateJavaScript:script completionHandler:^(id result,NSError *error){
  if(error)return;
  if(![result boolValue]) { if(attempt<60)dispatch_after(dispatch_time(DISPATCH_TIME_NOW,50*NSEC_PER_MSEC),dispatch_get_main_queue(),^{popchatProbePasteReady(pointer,marker,attempt+1);}); return; }
  NSEvent *event=[NSEvent keyEventWithType:NSEventTypeKeyDown location:NSZeroPoint modifierFlags:NSEventModifierFlagCommand timestamp:NSProcessInfo.processInfo.systemUptime windowNumber:window.windowNumber context:nil characters:@"v" charactersIgnoringModifiers:@"v" isARepeat:NO keyCode:9];
  if(![NSApp.mainMenu performKeyEquivalent:event])[NSApp sendEvent:event];
 }];
}
static void popchatProbePaste(void *pointer,const char *marker) {popchatProbePasteReady(pointer,[NSString stringWithUTF8String:marker],0);}
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"github.com/wailsapp/wails/v3/pkg/application"
	"os"
	"time"
	"unsafe"
)

func runClipboardProbe(d *Desktop) int {
	type result struct {
		Name   string `json:"name"`
		Pass   bool   `json:"pass"`
		Detail string `json:"detail,omitempty"`
	}
	results := []result{}
	wait := func(check func() bool) bool {
		deadline := time.Now().Add(4 * time.Second)
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
		return nativeProbeTrace.ready["main"] && nativeProbeTrace.ready["panel"]
	})
	results = append(results, result{"webviews-ready", ready, ""})
	application.InvokeSync(func() { C.popchatSaveClipboard() })
	defer application.InvokeSync(func() { C.popchatRestoreClipboard() })
	for _, view := range []string{"main", "panel"} {
		for kind, name := range []string{"text", "png", "tiff"} {
			sid, err := d.engine.NewSession(view, "")
			if err != nil {
				results = append(results, result{view + "-session", false, err.Error()})
				continue
			}
			marker := fmt.Sprintf("clipboard-%s-%s", view, name)
			_ = d.engine.Rename(sid, marker)
			w := d.main
			if view == "main" {
				d.hidePanel()
				d.showMain()
			} else {
				d.showPanel()
				w = d.panel
			}
			wait(func() bool { return w.IsFocused() })
			var prepared bool
			application.InvokeSync(func() {
				prepared = bool(C.popchatPrepareClipboard(C.int(kind)))
				if prepared {
					cm := C.CString(marker)
					C.popchatProbePaste(w.NativeWindow(), cm)
					C.free(unsafe.Pointer(cm))
				}
			})
			pass := prepared && wait(func() bool {
				s := d.engine.Snapshot(view).Current
				if s == nil {
					return false
				}
				if kind == 0 {
					return s.Draft == "clipboard-text-proof"
				}
				return len(s.DraftAttachments) == 1 && s.DraftAttachments[0].MIME == "image/png"
			})
			s := d.engine.Snapshot(view).Current
			detail := fmt.Sprintf("prepared=%v focused=%v selectedExpected=%v draftLength=%d attachments=%d", prepared, w.IsFocused(), s.ID == sid, len(s.Draft), len(s.DraftAttachments))
			if len(s.DraftAttachments) > 0 {
				detail += " mime=" + s.DraftAttachments[0].MIME
			}
			results = append(results, result{fmt.Sprintf("%s-command-v-%s", view, name), pass, detail})
		}
	}
	failed := 0
	for _, r := range results {
		if !r.Pass {
			failed++
		}
	}
	data, _ := json.MarshalIndent(map[string]any{"suite": "native-clipboard", "failed": failed, "results": results}, "", "  ")
	if p := os.Getenv("PI_POPCHAT_CHECK_OUTPUT"); p != "" {
		_ = os.WriteFile(p, data, 0600)
	}
	fmt.Println(string(data))
	return failed
}
