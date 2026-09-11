// Isolated desktop interaction acceptance. No product feature is installed.
// Only the dedicated child PID is queried; all keystrokes target its fixture.
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <WebKit/WebKit.h>

static CGFloat screenTop;
static id fixtureMouseMonitor;
static void mainSync(dispatch_block_t block) { dispatch_sync(dispatch_get_main_queue(),block); }
static void pauseFor(double seconds) { [NSThread sleepForTimeInterval:seconds]; }
static BOOL await(BOOL (^predicate)(void), double seconds) {
 double end=NSDate.timeIntervalSinceReferenceDate+seconds;
 do { if(predicate())return YES;pauseFor(.05); }while(NSDate.timeIntervalSinceReferenceDate<end);
 return NO;
}
static void json(NSDictionary *object,NSString *path) {
 NSData *data=[NSJSONSerialization dataWithJSONObject:object options:NSJSONWritingPrettyPrinted error:nil];
 if(path)[data writeToFile:path atomically:YES];
 else {fwrite(data.bytes,1,data.length,stdout);puts("");fflush(stdout);}
}
static CGPoint cgPoint(NSPoint p) { return CGPointMake(p.x,screenTop-p.y); }
static void mouse(CGEventType type,CGPoint p,int count) {
 CGEventRef event=CGEventCreateMouseEvent(NULL,type,p,kCGMouseButtonLeft);
 CGEventSetIntegerValueField(event,kCGMouseEventClickState,count);
 CGEventPost(kCGHIDEventTap,event);CFRelease(event);
}
static void click(CGPoint p,int count) { mouse(kCGEventLeftMouseDown,p,count);pauseFor(.06);mouse(kCGEventLeftMouseUp,p,count);pauseFor(.2); }
static void key(CGKeyCode code,CGEventFlags flags) {
 for(int down=1;down>=0;down--){CGEventRef e=CGEventCreateKeyboardEvent(NULL,code,down);CGEventSetFlags(e,flags);CGEventPost(kCGHIDEventTap,e);CFRelease(e);pauseFor(.04);}
 pauseFor(.2);
}
static NSDictionary *readSelection(pid_t pid) {
 AXUIElementRef app=AXUIElementCreateApplication(pid); AXUIElementSetMessagingTimeout(app,.4);
 CFTypeRef focused=NULL,text=NULL,range=NULL,bounds=NULL;
 AXError error=AXUIElementCopyAttributeValue(app,kAXFocusedUIElementAttribute,&focused);
 NSMutableDictionary *result=[NSMutableDictionary new];
 result[@"focusError"]=@(error);
 if(!focused && NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier==pid){
  AXUIElementRef system=AXUIElementCreateSystemWide();CFTypeRef candidate=NULL;
  AXError fallback=AXUIElementCopyAttributeValue(system,kAXFocusedUIElementAttribute,&candidate);CFRelease(system);
  pid_t owner=0;
  if(candidate&&CFGetTypeID(candidate)==AXUIElementGetTypeID())AXUIElementGetPid((AXUIElementRef)candidate,&owner);
  if(fallback==kAXErrorSuccess&&owner==pid){focused=candidate;error=kAXErrorSuccess;result[@"focusSource"]=@"system";}
  else if(candidate)CFRelease(candidate);
 }
 if(!focused){
  NSMutableArray *queue=[NSMutableArray arrayWithObject:(__bridge id)app];NSInteger index=0;
  while(index<(NSInteger)queue.count&&index<512){
   AXUIElementRef item=(__bridge AXUIElementRef)queue[index++];CFTypeRef flag=NULL,children=NULL;
   if(AXUIElementCopyAttributeValue(item,kAXFocusedAttribute,&flag)==kAXErrorSuccess&&flag){
    BOOL yes=CFEqual(flag,kCFBooleanTrue);CFRelease(flag);if(yes){focused=CFRetain(item);error=kAXErrorSuccess;result[@"focusSource"]=@"focused-descendant";break;}
   }
   if(AXUIElementCopyAttributeValue(item,kAXChildrenAttribute,&children)==kAXErrorSuccess&&children){
    if(CFGetTypeID(children)==CFArrayGetTypeID())for(id child in (__bridge NSArray*)children)if(CFGetTypeID((__bridge CFTypeRef)child)==AXUIElementGetTypeID()&&queue.count<1024)[queue addObject:child];
    CFRelease(children);
   }
  }
 }
 if(error==kAXErrorSuccess && focused && CFGetTypeID(focused)==AXUIElementGetTypeID()) {
  AXUIElementRef element=(AXUIElementRef)focused;
  CFTypeRef role=NULL;if(AXUIElementCopyAttributeValue(element,kAXRoleAttribute,&role)==kAXErrorSuccess){result[@"role"]=(__bridge id)role;CFRelease(role);}
  AXError te=AXUIElementCopyAttributeValue(element,kAXSelectedTextAttribute,&text);
  result[@"textError"]=@(te);
  if(te==kAXErrorSuccess && text && CFGetTypeID(text)==CFStringGetTypeID())result[@"text"]=(__bridge NSString*)text;
  if(AXUIElementCopyAttributeValue(element,kAXSelectedTextRangeAttribute,&range)==kAXErrorSuccess && range &&
     AXUIElementCopyParameterizedAttributeValue(element,kAXBoundsForRangeParameterizedAttribute,range,&bounds)==kAXErrorSuccess && bounds && CFGetTypeID(bounds)==AXValueGetTypeID()) {
   CGRect rect=CGRectZero;
   if(AXValueGetValue(bounds,kAXValueCGRectType,&rect) && rect.size.width>0 && rect.size.height>0)
   result[@"bounds"]=[NSValue valueWithRect:NSMakeRect(rect.origin.x,screenTop-CGRectGetMaxY(rect),rect.size.width,rect.size.height)];
  }
  // Read-only web content exposes selection through text markers, not AXSelectedText.
  AXUIElementRef candidate=(AXUIElementRef)CFRetain(element);
  for(int depth=0;depth<6 && candidate;depth++){
   CFTypeRef marker=NULL,markerText=NULL,markerBounds=NULL;
   AXError markerError=AXUIElementCopyAttributeValue(candidate,CFSTR("AXSelectedTextMarkerRange"),&marker);result[@"markerError"]=@(markerError);
   if(markerError==kAXErrorSuccess && marker){
    if(AXUIElementCopyParameterizedAttributeValue(candidate,CFSTR("AXStringForTextMarkerRange"),marker,&markerText)==kAXErrorSuccess && markerText && CFGetTypeID(markerText)==CFStringGetTypeID() && [(__bridge NSString*)markerText length]){
     result[@"text"]=(__bridge NSString*)markerText;result[@"selectionAPI"]=@"text-marker";
     if(AXUIElementCopyParameterizedAttributeValue(candidate,CFSTR("AXBoundsForTextMarkerRange"),marker,&markerBounds)==kAXErrorSuccess && markerBounds && CFGetTypeID(markerBounds)==AXValueGetTypeID()){
      CGRect rect=CGRectZero;if(AXValueGetValue(markerBounds,kAXValueCGRectType,&rect)&&rect.size.width>0&&rect.size.height>0)result[@"bounds"]=[NSValue valueWithRect:NSMakeRect(rect.origin.x,screenTop-CGRectGetMaxY(rect),rect.size.width,rect.size.height)];
     }
    }
   }
   if(marker)CFRelease(marker);if(markerText)CFRelease(markerText);if(markerBounds)CFRelease(markerBounds);
   if([result[@"text"] length]){CFRelease(candidate);candidate=NULL;break;}
   CFTypeRef parent=NULL;AXUIElementCopyAttributeValue(candidate,kAXParentAttribute,&parent);CFRelease(candidate);
   candidate=parent && CFGetTypeID(parent)==AXUIElementGetTypeID() ? (AXUIElementRef)parent : NULL;
   if(parent&&!candidate)CFRelease(parent);
  }
  if(candidate)CFRelease(candidate);
 }
 if(focused)CFRelease(focused);if(text)CFRelease(text);if(range)CFRelease(range);if(bounds)CFRelease(bounds);CFRelease(app);
 return result;
}

@interface SelectionHarness : NSObject
@property pid_t target;
@property BOOL enabled, permissionAllowed;
@property NSInteger buttonCount, globalUps, captures, submissions;
@property(strong) NSPanel *toolbar;
@property(copy) NSString *captured, *submitted;
@property(strong) id globalMonitor, localMonitor;
@end
@implementation SelectionHarness
- (instancetype)init {
 if((self=[super init])){
  self.enabled=YES;self.permissionAllowed=YES;self.buttonCount=1;
  self.toolbar=[[NSPanel alloc] initWithContentRect:NSMakeRect(0,0,160,38)
   styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];
  self.toolbar.level=NSFloatingWindowLevel;self.toolbar.hidesOnDeactivate=NO;
  self.toolbar.collectionBehavior=NSWindowCollectionBehaviorFullScreenAuxiliary|(1<<18);
  NSButton *button=[NSButton buttonWithTitle:@"Explain fixture" target:self action:@selector(submit:)];
  button.frame=NSMakeRect(4,4,152,30);[self.toolbar.contentView addSubview:button];
  __weak SelectionHarness *weak=self;
  NSEventMask mask=NSEventMaskLeftMouseDown|NSEventMaskLeftMouseUp|NSEventMaskKeyUp|NSEventMaskScrollWheel;
  self.globalMonitor=[NSEvent addGlobalMonitorForEventsMatchingMask:mask handler:^(NSEvent *e){[weak event:e];}];
  self.localMonitor=[NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskKeyUp handler:^NSEvent*(NSEvent *e){[weak event:e];return e;}];
 }
 return self;
}
- (void)hide { [self.toolbar orderOut:nil];self.captured=nil; }
- (void)event:(NSEvent*)event {
 if(event.type==NSEventTypeLeftMouseDown || event.type==NSEventTypeScrollWheel || (event.type==NSEventTypeKeyUp && event.keyCode==53)){[self hide];return;}
 if(event.type==NSEventTypeLeftMouseUp)self.globalUps++;
 if(!self.enabled || !self.permissionAllowed || !self.buttonCount || NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier!=self.target){[self hide];return;}
 NSDictionary *selection=readSelection(self.target);NSString *text=selection[@"text"];
 if(![text stringByTrimmingCharactersInSet:NSCharacterSet.whitespaceAndNewlineCharacterSet].length){[self hide];return;}
 self.captured=text;self.captures++;
 NSRect anchor=selection[@"bounds"] ? [selection[@"bounds"] rectValue] : NSMakeRect(NSEvent.mouseLocation.x,NSEvent.mouseLocation.y,1,1);
 NSScreen *screen=nil;for(NSScreen *s in NSScreen.screens)if(NSIntersectsRect(anchor,s.frame)){screen=s;break;}
 if(!screen)screen=NSScreen.mainScreen;
 NSRect area=screen.visibleFrame;
 NSPoint origin=NSMakePoint(MAX(NSMinX(area),MIN(NSMinX(anchor),NSMaxX(area)-160)),MAX(NSMinY(area),MIN(NSMinY(anchor)-42,NSMaxY(area)-38)));
 [self.toolbar setFrameOrigin:origin];[self.toolbar orderFrontRegardless];
}
- (void)submit:(id)sender {
 if(!self.enabled || !self.permissionAllowed || !self.buttonCount || !self.captured.length)return;
 self.submitted=self.captured;self.submissions++;[self hide];
}
@end

static void fixture(NSString *mode,int screenIndex,BOOL fullscreen,NSString *ready) {
 NSApplication *app=NSApplication.sharedApplication;
 NSMenu *menu=[NSMenu new];NSMenuItem *edit=[NSMenuItem new];NSMenu *submenu=[NSMenu new];
 [submenu addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];edit.submenu=submenu;[menu addItem:edit];app.mainMenu=menu;
 NSScreen *screen=NSScreen.screens[MIN(screenIndex,(int)NSScreen.screens.count-1)];
 NSRect area=screen.visibleFrame;
 NSRect initialFrame=NSMakeRect(NSMidX(area)-340,NSMidY(area)-160,680,320);
 NSWindow *window=[[NSWindow alloc] initWithContentRect:initialFrame
  styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskResizable backing:NSBackingStoreBuffered defer:NO];
 window.title=@"Popchat isolated selection acceptance";window.collectionBehavior=NSWindowCollectionBehaviorFullScreenPrimary;
 BOOL web=[mode hasPrefix:@"web"],secure=[mode isEqual:@"secure"];
 NSTextView *text=nil;WKWebView *webview=nil;
 if(web) {
  webview=[[WKWebView alloc] initWithFrame:window.contentView.bounds];webview.autoresizingMask=NSViewWidthSizable|NSViewHeightSizable;
  [window.contentView addSubview:webview];
  [webview loadHTMLString:[NSString stringWithFormat:@"<html><body style='margin:40px;font:20px monospace'><div id='fixture' %@>Synthetic selection</div><p>Second line</p></body></html>",[mode isEqual:@"web"]?@"contenteditable='true'":@""] baseURL:nil];
 }else if(secure){
  NSSecureTextField *field=[[NSSecureTextField alloc] initWithFrame:NSMakeRect(40,200,400,32)];field.stringValue=@"SYNTHETIC_NOT_A_PASSWORD";[window.contentView addSubview:field];
 }else{
  text=[[NSTextView alloc] initWithFrame:window.contentView.bounds];text.autoresizingMask=NSViewWidthSizable|NSViewHeightSizable;
  text.font=[NSFont monospacedSystemFontOfSize:20 weight:NSFontWeightRegular];text.textContainerInset=NSMakeSize(40,40);
  text.string=@"Synthetic selection\nSecond line";[window.contentView addSubview:text];
 }
 [app finishLaunching];[window setFrameOrigin:initialFrame.origin];[window makeKeyAndOrderFront:nil];[app activateIgnoringOtherApps:YES];
 if(text)[window makeFirstResponder:text];
 if(web){
  fixtureMouseMonitor=[NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskLeftMouseUp handler:^NSEvent*(NSEvent *event){
   dispatch_after(dispatch_time(DISPATCH_TIME_NOW,150*NSEC_PER_MSEC),dispatch_get_main_queue(),^{
    [webview evaluateJavaScript:@"window.getSelection().toString()" completionHandler:^(id value,NSError *error){if(!error)json(@{@"text":value?:@""},[ready stringByAppendingString:@".dom.json"]);}];
   });return event;
  }];
 }
 __block BOOL requested=NO;
 [NSTimer scheduledTimerWithTimeInterval:.1 repeats:YES block:^(NSTimer *timer){
  if(fullscreen && !(window.styleMask&NSWindowStyleMaskFullScreen)){if(!requested){requested=YES;[window toggleFullScreen:nil];}return;}
  if(fullscreen && window.inLiveResize)return;
  if(web && webview.loading)return;
  [timer invalidate];
  dispatch_after(dispatch_time(DISPATCH_TIME_NOW,700*NSEC_PER_MSEC),dispatch_get_main_queue(),^{
   void (^publish)(NSRect)=^(NSRect rect){
    // rect is in AppKit screen coordinates. Tests click and drag actual CG events.
    CGPoint start=cgPoint(NSMakePoint(NSMinX(rect)+1,NSMidY(rect)));
    CGPoint end=cgPoint(NSMakePoint(NSMaxX(rect)+2,NSMidY(rect)));
    CGPoint outside=cgPoint(NSMakePoint(NSMinX(window.frame)+25,NSMinY(window.frame)+25));
    json(@{@"start":@[@(start.x),@(start.y)],@"end":@[@(end.x),@(end.y)],@"outside":@[@(outside.x),@(outside.y)],@"fullscreen":@((window.styleMask&NSWindowStyleMaskFullScreen)!=0)},ready);
   };
   if(web){
    [webview evaluateJavaScript:@"(()=>{let r=document.createRange();r.selectNodeContents(document.getElementById('fixture'));let b=r.getBoundingClientRect();return [b.x,b.y,b.width,b.height]})()" completionHandler:^(id value,NSError *error){
     if(error||![value isKindOfClass:NSArray.class]){json(@{@"error":@"web geometry unavailable"},ready);return;}
     NSArray *a=value;CGFloat y=webview.isFlipped ? [a[1] doubleValue] : webview.bounds.size.height-[a[1] doubleValue]-[a[3] doubleValue];NSRect local=NSMakeRect([a[0] doubleValue],y,[a[2] doubleValue],[a[3] doubleValue]);
     publish([window convertRectToScreen:[webview convertRect:local toView:nil]]);
    }];
   }else if(secure){publish([window convertRectToScreen:NSMakeRect(40,200,250,32)]);}
   else{NSRect rect=[text firstRectForCharacterRange:NSMakeRange(0,19) actualRange:NULL];[text setSelectedRange:NSMakeRange(0,0)];publish(rect);}
  });
 }];
 [app run];
}

int main(int argc,const char *argv[]) {
 @autoreleasepool {
  if(argc<6){fprintf(stderr,"usage: selection-acceptance fixture|run MODE SCREEN FULLSCREEN READY\n");return 2;}
  NSApplication *app=NSApplication.sharedApplication;[app setActivationPolicy:NSApplicationActivationPolicyAccessory];
  screenTop=NSMaxY(NSScreen.screens.firstObject.frame);
  NSString *mode=@(argv[2]),*ready=@(argv[5]);int screenIndex=atoi(argv[3]);BOOL full=atoi(argv[4]);
  if(strcmp(argv[1],"permission")==0){json(@{@"accessibilityTrusted":@(AXIsProcessTrusted()),@"promptRequested":@NO,@"bundleIdentifier":NSBundle.mainBundle.bundleIdentifier?:@""},ready);return 0;}
  if(strcmp(argv[1],"fixture")==0){fixture(mode,screenIndex,full,ready);return 0;}
  [app finishLaunching];SelectionHarness *h=[SelectionHarness new];
  NSRunningApplication *previous=NSWorkspace.sharedWorkspace.frontmostApplication;
  CGEventRef current=CGEventCreate(NULL);CGPoint original=CGEventGetLocation(current);CFRelease(current);
  BOOL external=[mode isEqual:@"chrome"]||[mode isEqual:@"vscode"];
  NSTask *child=[NSTask new];child.executableURL=[NSURL fileURLWithPath:NSBundle.mainBundle.executablePath];child.arguments=@[@"fixture",mode,@(argv[3]),@(argv[4]),ready];
  if(external){
   // VS Code's Unix socket path is limited to 103 bytes on macOS.
   NSString *root=[NSString stringWithFormat:@"/tmp/pcsel-%d",getpid()];
   [[NSFileManager defaultManager] createDirectoryAtPath:root withIntermediateDirectories:YES attributes:nil error:nil];
   if([mode isEqual:@"chrome"]){
    NSString *html=[root stringByAppendingPathComponent:@"fixture.html"];
    [@"<html><head><title>Popchat isolated Chrome selection</title></head><body style='margin:40px;font:20px monospace'><div id='fixture' tabindex='0'>Synthetic selection</div><script>fixture.focus()</script></body></html>" writeToFile:html atomically:YES encoding:NSUTF8StringEncoding error:nil];
    child.executableURL=[NSURL fileURLWithPath:@"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"];
    child.arguments=@[[@"--user-data-dir=" stringByAppendingString:[root stringByAppendingPathComponent:@"profile"]],@"--no-first-run",@"--no-default-browser-check",@"--disable-background-networking",[@"--app=" stringByAppendingString:[NSURL fileURLWithPath:html].absoluteString]];
   }else{
    NSString *file=[root stringByAppendingPathComponent:@"fixture.txt"];
    [@"Synthetic selection" writeToFile:file atomically:YES encoding:NSUTF8StringEncoding error:nil];
    NSString *settings=[root stringByAppendingPathComponent:@"profile/User"];
    [[NSFileManager defaultManager] createDirectoryAtPath:settings withIntermediateDirectories:YES attributes:nil error:nil];
    json(@{@"workbench.startupEditor":@"none",@"telemetry.telemetryLevel":@"off",@"update.mode":@"none",@"editor.minimap.enabled":@NO},[settings stringByAppendingPathComponent:@"settings.json"]);
    child.executableURL=[NSURL fileURLWithPath:@"/Applications/Visual Studio Code.app/Contents/MacOS/Electron"];
    child.arguments=@[@"--user-data-dir",[root stringByAppendingPathComponent:@"profile"],@"--extensions-dir",[root stringByAppendingPathComponent:@"extensions"],@"--disable-extensions",@"--skip-welcome",@"--skip-release-notes",@"--new-window",file];
   }
   child.standardOutput=[NSFileHandle fileHandleWithNullDevice];
  }
  if(![child launchAndReturnError:nil])return 1;h.target=child.processIdentifier;
  dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED,0),^{
   NSMutableArray *results=[NSMutableArray new];
   void (^record)(NSString*,BOOL,NSString*)=^(NSString *name,BOOL pass,NSString *detail){[results addObject:@{@"name":name,@"pass":@(pass),@"detail":detail?:@""}];fprintf(stderr,"%s %s\n",pass?"PASS":"FAIL",name.UTF8String);};
   if(external){
    BOOL active=await(^BOOL{
     mainSync(^{[[NSRunningApplication runningApplicationWithProcessIdentifier:child.processIdentifier] activateWithOptions:0];});
     return NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier==child.processIdentifier;
    },10);
    if(active){
     AXUIElementRef axApp=AXUIElementCreateApplication(child.processIdentifier);AXUIElementSetMessagingTimeout(axApp,.4);
     AXUIElementSetAttributeValue(axApp,CFSTR("AXEnhancedUserInterface"),kCFBooleanTrue);
     if([mode isEqual:@"vscode"])AXUIElementSetAttributeValue(axApp,CFSTR("AXManualAccessibility"),kCFBooleanTrue);
     await(^BOOL{CFTypeRef w=NULL;AXError e=AXUIElementCopyAttributeValue(axApp,kAXFocusedWindowAttribute,&w);if(w)CFRelease(w);return e==kAXErrorSuccess;},10);
     CFTypeRef window=NULL;
     if(AXUIElementCopyAttributeValue(axApp,kAXFocusedWindowAttribute,&window)==kAXErrorSuccess&&window){
      __block NSRect area;mainSync(^{area=NSScreen.screens.firstObject.visibleFrame;});
      CGPoint position=cgPoint(NSMakePoint(NSMidX(area)-400,NSMidY(area)+250));CGSize size=CGSizeMake(800,500);
      AXValueRef p=AXValueCreate(kAXValueCGPointType,&position),s=AXValueCreate(kAXValueCGSizeType,&size);
      AXUIElementSetAttributeValue((AXUIElementRef)window,kAXPositionAttribute,p);AXUIElementSetAttributeValue((AXUIElementRef)window,kAXSizeAttribute,s);CFRelease(p);CFRelease(s);CFRelease(window);
     }
     CFRelease(axApp);pauseFor(2);
     __block NSDictionary *selection;
     BOOL located=await(^BOOL{
      if(NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier!=child.processIdentifier){
       CFArrayRef windows=CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly,kCGNullWindowID);
       for(NSDictionary *w in (__bridge NSArray*)windows){
        if([w[(__bridge NSString*)kCGWindowOwnerPID] intValue]==child.processIdentifier && [w[(__bridge NSString*)kCGWindowLayer] intValue]==0){
         CGRect rect;if(CGRectMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)w[(__bridge NSString*)kCGWindowBounds],&rect))click(CGPointMake(rect.origin.x+300,rect.origin.y+14),1);break;
        }
       }if(windows)CFRelease(windows);
      }
      if(NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier!=child.processIdentifier)return NO;
      key(0,kCGEventFlagMaskCommand);selection=readSelection(child.processIdentifier);
      return [selection[@"text"] isEqual:@"Synthetic selection"]&&selection[@"bounds"]!=nil;
     },10);
     if(located){
      NSRect rect=[selection[@"bounds"] rectValue];CGPoint start=cgPoint(NSMakePoint(NSMinX(rect)+1,NSMidY(rect))),end=cgPoint(NSMakePoint(NSMaxX(rect)+2,NSMidY(rect))),outside=cgPoint(NSMakePoint(NSMinX(rect),NSMinY(rect)-80));
      json(@{@"start":@[@(start.x),@(start.y)],@"end":@[@(end.x),@(end.y)],@"outside":@[@(outside.x),@(outside.y)],@"fullscreen":@NO},ready);
     }else{
      record(@"external-selection-discovery",NO,[NSString stringWithFormat:@"role=%@ focusError=%@ textError=%@ length=%lu frontPID=%d targetPID=%d",selection[@"role"],selection[@"focusError"],selection[@"textError"],(unsigned long)[selection[@"text"] length],NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier,child.processIdentifier]);
      CFArrayRef windows=CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly,kCGNullWindowID);
      for(NSDictionary *w in (__bridge NSArray*)windows){
       if([w[(__bridge NSString*)kCGWindowOwnerPID] intValue]==child.processIdentifier && [w[(__bridge NSString*)kCGWindowLayer] intValue]==0){
        NSTask *shot=[NSTask new];shot.executableURL=[NSURL fileURLWithPath:@"/usr/sbin/screencapture"];
        shot.arguments=@[@"-x",[@"-l" stringByAppendingString:[w[(__bridge NSString*)kCGWindowNumber] stringValue]],[ready stringByAppendingString:@".png"]];
        [shot launchAndReturnError:nil];[shot waitUntilExit];break;
       }
      }if(windows)CFRelease(windows);
     }
    }
   }
   BOOL readyOK=await(^BOOL{return [[NSFileManager defaultManager] fileExistsAtPath:ready];},external?1:15);
   NSDictionary *geometry=readyOK?[NSJSONSerialization JSONObjectWithData:[NSData dataWithContentsOfFile:ready] options:0 error:nil]:nil;
   record(@"fixture-ready",readyOK && geometry[@"start"]!=nil,@"");
   record(@"accessibility-permission",AXIsProcessTrusted(),@"Actual probe identity; not proof of product app authorization");
   if(geometry[@"start"] && AXIsProcessTrusted()){
    AXUIElementRef bootstrap=AXUIElementCreateApplication(child.processIdentifier);
    AXUIElementSetAttributeValue(bootstrap,CFSTR("AXEnhancedUserInterface"),kCFBooleanTrue);
    CFRelease(bootstrap);readSelection(child.processIdentifier);pauseFor(.5);
    NSArray *s=geometry[@"start"],*e=geometry[@"end"],*o=geometry[@"outside"];
    CGPoint start=CGPointMake([s[0] doubleValue],[s[1] doubleValue]),end=CGPointMake([e[0] doubleValue],[e[1] doubleValue]),outside=CGPointMake([o[0] doubleValue],[o[1] doubleValue]);
    BOOL (^visible)(void)=^BOOL{__block BOOL v;mainSync(^{v=h.toolbar.visible;});return v;};
    NSString *(^selected)(void)=^NSString*{return readSelection(child.processIdentifier)[@"text"]?:@"";};
    void (^drag)(void)=^{
     mouse(kCGEventLeftMouseDown,start,1);pauseFor(.08);
     for(int i=1;i<=12;i++){mouse(kCGEventLeftMouseDragged,CGPointMake(start.x+(end.x-start.x)*i/12.,start.y+(end.y-start.y)*i/12.),1);pauseFor(.025);}
     mouse(kCGEventLeftMouseUp,end,1);pauseFor(.3);
    };
    await(^BOOL{return NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier==child.processIdentifier;},3);
    drag();
    if([mode isEqual:@"secure"]){
     record(@"secure-field-not-exposed",selected().length==0,@"Only synthetic secure field queried");
     record(@"secure-field-no-toolbar",!visible(),@"");
    }else{
     NSDictionary *selectionDetail=readSelection(child.processIdentifier);
     NSData *domData=[NSData dataWithContentsOfFile:[ready stringByAppendingString:@".dom.json"]];NSDictionary *dom=domData?[NSJSONSerialization JSONObjectWithData:domData options:0 error:nil]:nil;
     record(@"real-drag-selection",[selected() isEqual:@"Synthetic selection"],[NSString stringWithFormat:@"selectedLength=%lu role=%@ textError=%@ markerError=%@ selectionAPI=%@ DOMMatches=%@",(unsigned long)selected().length,selectionDetail[@"role"],selectionDetail[@"textError"],selectionDetail[@"markerError"],selectionDetail[@"selectionAPI"]?:@"standard",dom?@([dom[@"text"] isEqual:@"Synthetic selection"]):@"n/a"]);
     record(@"global-mouse-up-delivered",h.globalUps>0,@"CG mouse events delivered through system event pipeline; not physical hardware input");
     record(@"automatic-toolbar",await(visible,2),@"");
     record(@"source-keeps-focus",NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier==child.processIdentifier,@"");
     __block BOOL sameScreen=NO,contained=NO,onSpace=NO;__block CGPoint button;
     mainSync(^{NSRect f=h.toolbar.frame;NSScreen *target=NSScreen.screens[MIN(screenIndex,(int)NSScreen.screens.count-1)];sameScreen=h.toolbar.screen==target;contained=NSContainsRect(target.visibleFrame,f);onSpace=h.toolbar.onActiveSpace;button=cgPoint(NSMakePoint(NSMidX(f),NSMidY(f)));});
     record(@"toolbar-on-source-screen",sameScreen,@"");record(@"toolbar-within-visible-frame",contained,@"");
     if(full)record(@"toolbar-in-fullscreen-space",onSpace&&[geometry[@"fullscreen"] boolValue],@"");
     if(visible()){click(button,1);pauseFor(.2);}
     record(@"click-submits-captured-selection",h.submissions==1&&[h.submitted isEqual:@"Synthetic selection"],@"Captured callback only; no model or real message sent");
     record(@"click-hides-toolbar",!visible(),@"");
     record(@"click-preserves-source-selection",[selected() isEqual:@"Synthetic selection"],@"");
     drag();click(outside,1);record(@"outside-click-dismisses",!visible(),@"");
     mainSync(^{h.enabled=NO;[h hide];});drag();record(@"disabled-no-toolbar",!visible(),@"");
     mainSync(^{h.enabled=YES;h.buttonCount=0;});drag();record(@"zero-buttons-no-toolbar",!visible(),@"");
     mainSync(^{h.buttonCount=1;h.permissionAllowed=NO;});drag();record(@"permission-gate-no-toolbar",!visible(),@"Injected denied gate, not real TCC revocation");
     mainSync(^{h.permissionAllowed=YES;});
     click(start,1);pauseFor(.25);click(start,2);pauseFor(.4);
     record(@"double-click-word",[selected() isEqual:@"Synthetic"]&&visible(),[NSString stringWithFormat:@"selectedLength=%lu",(unsigned long)selected().length]);
     mainSync(^{[h hide];});key(0,kCGEventFlagMaskCommand);
     record(@"keyboard-selection-toolbar",await(visible,2)&&[selected() containsString:@"Synthetic selection"],@"Cmd+A on dedicated fixture");
     mainSync(^{[h hide];});
     __block NSInteger before;mainSync(^{before=h.submissions;[h submit:nil];});
     record(@"stale-hidden-selection-not-submitted",h.submissions==before,@"");
     drag();key(53,0);record(@"escape-dismisses",await(^BOOL{return !visible();},2),@"Source application may also handle Escape, including leaving its full-screen Space");
    }
   }
   mainSync(^{[h hide];[NSEvent removeMonitor:h.globalMonitor];[NSEvent removeMonitor:h.localMonitor];});
   if(child.running)[child terminate];[child waitUntilExit];
   CGWarpMouseCursorPosition(original);mainSync(^{[previous activateWithOptions:0];});
   NSUInteger failures=0;for(NSDictionary *r in results)if(![r[@"pass"] boolValue])failures++;
   json(@{@"suite":@"selection-interaction-prototype",@"mode":mode,@"screenIndex":@(screenIndex),@"fullscreen":@(full),@"failed":@(failures),@"results":results},nil);
   exit(failures?1:0);
  });
  [app run];
 }
 return 0;
}
