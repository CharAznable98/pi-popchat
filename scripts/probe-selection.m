// Standalone feasibility probe. Reads only its own synthetic child window.
// No permission prompts, clipboard access, real user text or model requests.
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <WebKit/WebKit.h>

static void writeJSON(NSDictionary *value, NSString *path) {
 NSData *data=[NSJSONSerialization dataWithJSONObject:value options:NSJSONWritingPrettyPrinted error:nil];
 if(path) [data writeToFile:path atomically:YES];
 else { fwrite(data.bytes,1,data.length,stdout); puts(""); fflush(stdout); }
}

int main(int argc, const char *argv[]) {
 @autoreleasepool {
  NSApplication *app=NSApplication.sharedApplication;
  [app setActivationPolicy:NSApplicationActivationPolicyAccessory];
  NSString *ready=argc>2 ? @(argv[2]) : nil;
  BOOL web=argc>3 && strcmp(argv[3],"web")==0;
  if(argc>1 && strcmp(argv[1],"fixture")==0) {
   NSWindow *window=[[NSWindow alloc] initWithContentRect:NSMakeRect(160,180,540,240)
    styleMask:NSWindowStyleMaskTitled backing:NSBackingStoreBuffered defer:NO];
   window.title=@"Popchat synthetic selection probe";
   NSTextView *text=[[NSTextView alloc] initWithFrame:NSMakeRect(10,10,510,200)];
   text.string=@"Synthetic selection\nSecond line";
   [window.contentView addSubview:text];
   [app finishLaunching];
   [window makeKeyAndOrderFront:nil]; [window makeFirstResponder:text];
   [app activateIgnoringOtherApps:YES];
   [text setSelectedRange:NSMakeRange(0,19)];
   WKWebView *webview=nil;
   if(web) {
    webview=[[WKWebView alloc] initWithFrame:NSMakeRect(10,10,510,200)];
    [text removeFromSuperview]; [window.contentView addSubview:webview];
    [webview loadHTMLString:@"<html><body><div id='fixture' contenteditable='true'>Synthetic selection</div></body></html>" baseURL:nil];
   }
   dispatch_after(dispatch_time(DISPATCH_TIME_NOW,500*NSEC_PER_MSEC),dispatch_get_main_queue(),^{
    if(web) {
     [webview evaluateJavaScript:@"const e=document.getElementById('fixture');e.focus();const r=document.createRange();r.selectNodeContents(e);const s=window.getSelection();s.removeAllRanges();s.addRange(r);s.toString()==='Synthetic selection'" completionHandler:^(id value,NSError *error){
      writeJSON(@{@"webDOMSelectionVerified":@(!error && [value boolValue])},ready);
     }];
     return;
    }
    BOOL selected=[[text accessibilitySelectedText] isEqualToString:@"Synthetic selection"];
    NSRange range=[text selectedRange];
    NSRect bounds=[text firstRectForCharacterRange:range actualRange:NULL];
    NSPanel *panel=[[NSPanel alloc] initWithContentRect:NSMakeRect(180,420,200,40)
     styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel
     backing:NSBackingStoreBuffered defer:NO];
    panel.level=NSFloatingWindowLevel; panel.hidesOnDeactivate=NO;
    [panel orderFrontRegardless];
    BOOL preserved=NSEqualRanges(text.selectedRange,range) && window.firstResponder==text;
    writeJSON(@{@"fixtureSelectedText":@(selected),@"fixtureRangeBounds":@(bounds.size.width>0 && bounds.size.height>0),
     @"sameProcessNonactivatingPanelPreservesSelection":@(preserved)},ready);
    // Parent performs cross-process AX access while this synthetic text is focused.
   });
   [app run]; return 0;
  }
  if(!ready) { fprintf(stderr,"usage: probe-selection probe READY_PATH\n"); return 2; }
  NSRunningApplication *previous=NSWorkspace.sharedWorkspace.frontmostApplication;
  [app finishLaunching];
  NSTask *fixture=[NSTask new];
  fixture.executableURL=[NSURL fileURLWithPath:NSBundle.mainBundle.executablePath];
  fixture.arguments=@[@"fixture",ready,web ? @"web" : @"native"];
  NSError *launchError=nil;
  if(![fixture launchAndReturnError:&launchError]) { fprintf(stderr,"fixture launch failed\n");return 1; }
  id monitor=[NSEvent addGlobalMonitorForEventsMatchingMask:NSEventMaskLeftMouseUp handler:^(NSEvent *event){}];
  __block int attempts=0;
  [NSTimer scheduledTimerWithTimeInterval:0.1 repeats:YES block:^(NSTimer *timer){
   NSData *data=[NSData dataWithContentsOfFile:ready];
   if(!data && ++attempts<100)return;
   [timer invalidate];
   NSMutableDictionary *report=data ? [[NSJSONSerialization JSONObjectWithData:data options:0 error:nil] mutableCopy] : [NSMutableDictionary new];
   report[@"fixtureReady"]=@(data!=nil);
   report[@"accessibilityTrusted"]=@(AXIsProcessTrusted());
   report[@"globalMouseMonitorRegistered"]=@(monitor!=nil);
   report[@"globalMouseDeliveryVerified"]=@NO;
   report[@"screenCount"]=@(NSScreen.screens.count);
   report[@"systemLanguage"]=NSLocale.preferredLanguages.firstObject ?: @"";
   report[@"systemTimezone"]=NSTimeZone.localTimeZone.name;
   AXUIElementRef target=AXUIElementCreateApplication(fixture.processIdentifier);
   AXUIElementSetMessagingTimeout(target,1);
   CFTypeRef focused=NULL;
   AXError error=AXUIElementCopyAttributeValue(target,kAXFocusedUIElementAttribute,&focused);
   report[@"focusedElementAXError"]=@(error);
   if(error==kAXErrorSuccess && focused && CFGetTypeID(focused)==AXUIElementGetTypeID()) {
    CFTypeRef value=NULL;
    AXError selectedError=AXUIElementCopyAttributeValue((AXUIElementRef)focused,kAXSelectedTextAttribute,&value);
    report[@"crossProcessSelectedTextVerified"]=@(selectedError==kAXErrorSuccess && [(__bridge id)value isEqual:@"Synthetic selection"]);
    if(value)CFRelease(value);
    CFTypeRef range=NULL, bounds=NULL;
    AXError rangeError=AXUIElementCopyAttributeValue((AXUIElementRef)focused,kAXSelectedTextRangeAttribute,&range);
    AXError boundsError=rangeError==kAXErrorSuccess ? AXUIElementCopyParameterizedAttributeValue((AXUIElementRef)focused,kAXBoundsForRangeParameterizedAttribute,range,&bounds) : rangeError;
    CGRect rect=CGRectZero;
    BOOL valid=bounds && CFGetTypeID(bounds)==AXValueGetTypeID() && AXValueGetValue(bounds,kAXValueCGRectType,&rect);
    report[@"crossProcessBoundsVerified"]=@(boundsError==kAXErrorSuccess && valid && rect.size.width>0 && rect.size.height>0);
    report[@"boundsAXError"]=@(boundsError);
    if(range)CFRelease(range); if(bounds)CFRelease(bounds);
   } else {
    report[@"crossProcessSelectedTextVerified"]=@NO;
    report[@"crossProcessBoundsVerified"]=@NO;
   }
   if(focused)CFRelease(focused); CFRelease(target);
   report[@"scope"]=web ? @"Synthetic WKWebView; no real browser or Electron acceptance" : @"Synthetic NSTextView; no full-screen or physical mouse delivery acceptance";
   if(monitor)[NSEvent removeMonitor:monitor];
   if(fixture.running)[fixture terminate];
   [previous activateWithOptions:0];
   writeJSON(report,nil);
   exit(data ? 0 : 1);
  }];
  [app run];
 }
 return 0;
}
