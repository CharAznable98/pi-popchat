#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <NaturalLanguage/NaturalLanguage.h>
#import "selection_toolbar_darwin.h"
extern void popchatSelectionClicked(char *payload);
extern void popchatSelectionPermissionChanged(void);

// All AX work is serialized away from AppKit; UI and generation state stay on
// the main queue. No clipboard, OCR, or surrounding text is read.
static id attr(AXUIElementRef element, CFStringRef key) {
 CFTypeRef value=NULL;
 if(AXUIElementCopyAttributeValue(element,key,&value)!=kAXErrorSuccess||!value)return nil;
 return [(id)value autorelease];
}
static id parameter(AXUIElementRef element,CFStringRef key,id value) {
 CFTypeRef out=NULL;if(!value)return nil;
 if(AXUIElementCopyParameterizedAttributeValue(element,key,(CFTypeRef)value,&out)!=kAXErrorSuccess||!out)return nil;
 return [(id)out autorelease];
}
static BOOL elementValue(id v){return v&&CFGetTypeID((CFTypeRef)v)==AXUIElementGetTypeID();}
static BOOL secure(AXUIElementRef element) {
 id sub=attr(element,kAXSubroleAttribute),role=attr(element,kAXRoleAttribute);
 return [sub isEqual:@"AXSecureTextField"]||[role isEqual:@"AXSecureTextField"]||[attr(element,CFSTR("AXProtectedContent")) isEqual:@YES];
}
static NSDictionary *captureSelection(pid_t pid) {
 AXUIElementRef app=AXUIElementCreateApplication(pid);AXUIElementSetMessagingTimeout(app,.15);
 // Chromium/Electron publish the accessibility tree only after enabling it.
 AXUIElementSetAttributeValue(app,CFSTR("AXManualAccessibility"),kCFBooleanTrue);
 AXUIElementSetAttributeValue(app,CFSTR("AXEnhancedUserInterface"),kCFBooleanTrue);
 CFAbsoluteTime deadline=CFAbsoluteTimeGetCurrent()+1.2;
 id focused=attr(app,kAXFocusedUIElementAttribute);
 if(!elementValue(focused)) {
  AXUIElementRef system=AXUIElementCreateSystemWide();AXUIElementSetMessagingTimeout(system,.15);
  id candidate=attr(system,kAXFocusedUIElementAttribute);pid_t owner=0;
  if(elementValue(candidate))AXUIElementGetPid((AXUIElementRef)candidate,&owner);
  if(owner==pid)focused=candidate;CFRelease(system);
 }
 if(!elementValue(focused)) {
  NSMutableArray *queue=[NSMutableArray arrayWithObject:(id)app];NSUInteger i=0;
  while(i<queue.count&&i<128&&CFAbsoluteTimeGetCurrent()<deadline){
   AXUIElementRef node=(AXUIElementRef)queue[i++];if(secure(node))continue;
   if([attr(node,kAXFocusedAttribute) isEqual:@YES]){focused=(id)node;break;}
   id children=attr(node,kAXChildrenAttribute);if([children isKindOfClass:NSArray.class])for(id child in children)if(elementValue(child)&&queue.count<256)[queue addObject:child];
  }
 }
 NSMutableDictionary *result=[NSMutableDictionary dictionary];
 if(elementValue(focused)) {
  // Inspect security before either selected-text API, including ancestor fields.
  id candidate=focused;BOOL protected=NO;
  for(int depth=0;depth<8&&elementValue(candidate);depth++){
   if(secure((AXUIElementRef)candidate)){protected=YES;break;}
   candidate=attr((AXUIElementRef)candidate,kAXParentAttribute);
  }
  if(!protected){
   candidate=focused;
   for(int depth=0;depth<8&&elementValue(candidate)&&CFAbsoluteTimeGetCurrent()<deadline;depth++){
    AXUIElementRef node=(AXUIElementRef)candidate;
    id text=attr(node,kAXSelectedTextAttribute),bounds=nil;
    if([text isKindOfClass:NSString.class]&&[text length]) {
     bounds=parameter(node,kAXBoundsForRangeParameterizedAttribute,attr(node,kAXSelectedTextRangeAttribute));
    }else{
     id marker=attr(node,CFSTR("AXSelectedTextMarkerRange"));
     text=parameter(node,CFSTR("AXStringForTextMarkerRange"),marker);
     bounds=parameter(node,CFSTR("AXBoundsForTextMarkerRange"),marker);
    }
    if([text isKindOfClass:NSString.class]&&[text length]&&[text length]<=200000){
     result[@"text"]=text;CGRect rect;
     if(bounds&&CFGetTypeID((CFTypeRef)bounds)==AXValueGetTypeID()&&AXValueGetValue((AXValueRef)bounds,kAXValueCGRectType,&rect)&&rect.size.width>0&&rect.size.height>0) result[@"bounds"]=[NSValue valueWithRect:NSRectFromCGRect(rect)];
     break;
    }
    candidate=attr(node,kAXParentAttribute);
   }
  }
 }
 CFRelease(app);return result;
}
@interface PopchatSelection : NSObject
@property(retain) NSPanel *panel;
@property(retain) NSDictionary *config;
@property(retain) NSDictionary *captured;
@property(retain) id globalMonitor;
@property(retain) id localMonitor;
@property(retain) id workspaceMonitor;
@property(retain) NSTimer *permissionTimer;
@property BOOL monitoredPermission;
@property NSUInteger generation;
@property BOOL reading;
@property(assign) dispatch_queue_t reader;
@end
@implementation PopchatSelection
- (instancetype)init {
 if((self=[super init])){
  self.reader=dispatch_queue_create("popchat.selection",DISPATCH_QUEUE_SERIAL);
  self.panel=[[[NSPanel alloc]initWithContentRect:NSMakeRect(0,0,180,40) styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO] autorelease];
  self.panel.level=NSFloatingWindowLevel;self.panel.hidesOnDeactivate=NO;self.panel.hasShadow=YES;
  self.panel.collectionBehavior=NSWindowCollectionBehaviorCanJoinAllSpaces|NSWindowCollectionBehaviorFullScreenAuxiliary|(1<<18);
  self.panel.opaque=NO;
  self.panel.backgroundColor=NSColor.clearColor;
  self.panel.acceptsMouseMovedEvents=YES;
  self.panel.animationBehavior=NSWindowAnimationBehaviorUtilityWindow;
  NSEventMask mask=NSEventMaskLeftMouseDown|NSEventMaskLeftMouseUp|NSEventMaskRightMouseDown|NSEventMaskKeyDown|NSEventMaskKeyUp|NSEventMaskScrollWheel;
  self.monitoredPermission = AXIsProcessTrusted();
  self.permissionTimer = [NSTimer scheduledTimerWithTimeInterval:2 repeats:YES block:^(NSTimer *timer) { [self refreshMonitor]; }];
  // keyCode is invalid for mouse/scroll events and raises an AppKit exception.
  // Check the event type before reading keyboard-only properties.
  self.localMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:mask
    handler:^NSEvent *(NSEvent *event) {
      if (event.window != self.panel) [self hide];
      if (event.type == NSEventTypeKeyDown && event.keyCode == 53) [self hide];
      return event;
    }];
  self.workspaceMonitor=[NSWorkspace.sharedWorkspace.notificationCenter addObserverForName:NSWorkspaceDidActivateApplicationNotification object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note){[self hide];}];
 }
 return self;
}
- (void)refreshMonitor {
 BOOL trusted = AXIsProcessTrusted();
 BOOL changed = trusted != self.monitoredPermission;
 self.monitoredPermission = trusted;
 BOOL enabled = [self.config[@"enabled"] boolValue] && [self.config[@"buttons"] count] > 0;
 if (changed || !enabled) {
  [self hide];
  if (self.globalMonitor) [NSEvent removeMonitor:self.globalMonitor];
  self.globalMonitor = nil;
 }
 if (enabled && !self.globalMonitor) {
  NSEventMask mask = NSEventMaskLeftMouseDown | NSEventMaskLeftMouseUp |
                      NSEventMaskRightMouseDown | NSEventMaskScrollWheel;
  // Keyboard monitoring needs accessibility permission. Re-register after the
  // permission changes instead of retaining a monitor created before consent.
  if (trusted) mask |= NSEventMaskKeyDown | NSEventMaskKeyUp;
  self.globalMonitor = [NSEvent addGlobalMonitorForEventsMatchingMask:mask
    handler:^(NSEvent *event) { [self event:event]; }];
 }
 if (changed) popchatSelectionPermissionChanged();
}
- (void)hide {self.generation++;self.captured=nil;[self.panel orderOut:nil];}
- (void)event:(NSEvent*)event {
 if(event.type!=NSEventTypeLeftMouseUp&&!(event.type==NSEventTypeKeyUp&&(event.modifierFlags&NSEventModifierFlagShift||event.keyCode==0&&(event.modifierFlags&NSEventModifierFlagCommand)))){[self hide];return;}
 [self hide];NSUInteger ticket=self.generation;
 if(![self.config[@"enabled"] boolValue]||![self.config[@"buttons"] count]||!AXIsProcessTrusted())return;
 pid_t pid=NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier;
 if(pid==NSProcessInfo.processInfo.processIdentifier)return;
 NSPoint mouse=NSEvent.mouseLocation;
 dispatch_after(dispatch_time(DISPATCH_TIME_NOW,180*NSEC_PER_MSEC),dispatch_get_main_queue(),^{
  [self readTicket:ticket pid:pid mouse:mouse];
 });
}
- (void)readTicket:(NSUInteger)ticket pid:(pid_t)pid mouse:(NSPoint)mouse {
 if(ticket!=self.generation)return;
 if(self.reading){dispatch_after(dispatch_time(DISPATCH_TIME_NOW,100*NSEC_PER_MSEC),dispatch_get_main_queue(),^{[self readTicket:ticket pid:pid mouse:mouse];});return;}
 self.reading=YES;
 dispatch_async(self.reader,^{@autoreleasepool{
  NSDictionary *selected=[captureSelection(pid) copy];
  dispatch_async(dispatch_get_main_queue(),^{
   self.reading=NO;
   if(ticket==self.generation&&pid==NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier&&[selected[@"text"] length]){self.captured=selected;[self showAt:mouse];}
   [selected release];
  });
 }});
}

- (void)showAt:(NSPoint)mouse {
 NSPoint anchor=mouse;id bounds=self.captured[@"bounds"];
 if(bounds){NSRect r=[bounds rectValue];anchor=NSMakePoint(NSMinX(r),NSMaxY(NSScreen.screens.firstObject.frame)-NSMaxY(r));}
 NSScreen *screen=NSScreen.mainScreen;for(NSScreen *s in NSScreen.screens)if(NSPointInRect(anchor,s.frame)){screen=s;break;}
 NSView *toolbar = popchatSelectionToolbar(self.config[@"buttons"],
     MIN(520, screen.visibleFrame.size.width-16), self, @selector(click:));
 CGFloat width = toolbar.frame.size.width, height = toolbar.frame.size.height;
 NSRect visible=screen.visibleFrame;CGFloat px=MIN(MAX(anchor.x,NSMinX(visible)+4),NSMaxX(visible)-width-4);
 CGFloat py=anchor.y-height-8;if(py<NSMinY(visible))py=MIN(anchor.y+24,NSMaxY(visible)-height-4);
 // Assigning a content view first resizes it to the previous window dimensions.
 [self.panel setFrame:NSMakeRect(px,py,width,height) display:NO];
 self.panel.contentView = toolbar;
 [self.panel invalidateShadow];
 [self.panel orderFrontRegardless];
}
- (void)click:(NSButton*)sender {
 if(!self.captured||sender.tag>=[self.config[@"buttons"] count])return;
 NSDictionary *button=self.config[@"buttons"][sender.tag];NSString *text=[[self.captured[@"text"] copy] autorelease];
 NSDate *clicked=NSDate.date;NSString *timezone=NSTimeZone.localTimeZone.name;
 NSArray *languages=NSLocale.preferredLanguages;NSString *language=languages.firstObject?:NSLocale.currentLocale.localeIdentifier;
 NSString *systemLanguage=language;
 if([button[@"defaultTranslation"] boolValue]){
  NSString *detected=[NLLanguageRecognizer dominantLanguageForString:text];
  NSString *base=[[language componentsSeparatedByCharactersInSet:[NSCharacterSet characterSetWithCharactersInString:@"-_"]] firstObject];
  NSString *detectedBase=[[detected componentsSeparatedByString:@"-"] firstObject];
  if([base isEqual:detectedBase]){
   if(![base isEqual:@"en"])language=@"en";
   else {
    language=nil;for(NSString *candidate in languages){if(![[candidate componentsSeparatedByString:@"-"].firstObject isEqual:@"en"]){language=candidate;break;}}
    if(!language){
     [self hide];NSAlert *alert=[[[NSAlert alloc]init]autorelease];alert.messageText=@"选择翻译目标语言";
     NSComboBox *input=[[[NSComboBox alloc]initWithFrame:NSMakeRect(0,0,260,28)]autorelease];[input addItemsWithObjectValues:@[@"zh-Hans",@"ja",@"ko",@"fr",@"de",@"es"]];input.stringValue=@"zh-Hans";alert.accessoryView=input;[alert addButtonWithTitle:@"翻译"];[alert addButtonWithTitle:@"取消"];
     [NSApp activateIgnoringOtherApps:YES];if([alert runModal]!=NSAlertFirstButtonReturn)return;
     language=[input.stringValue stringByTrimmingCharactersInSet:NSCharacterSet.whitespaceAndNewlineCharacterSet];if(!language.length)return;
    }
   }
  }
 }
 NSString *template=button[@"template"];
 if([button[@"defaultTranslation"] boolValue])template=[template stringByReplacingOccurrencesOfString:@"{{language}}" withString:language];
 NSDictionary *payload=@{@"text":text,@"template":template,@"language":systemLanguage,@"time":@((long long)(clicked.timeIntervalSince1970*1000)),@"timezone":timezone,@"timezoneOffset":@([NSTimeZone.localTimeZone secondsFromGMTForDate:clicked])};
 NSData *data=[NSJSONSerialization dataWithJSONObject:payload options:0 error:nil];NSString *json=[[[NSString alloc]initWithData:data encoding:NSUTF8StringEncoding]autorelease];
 [self hide];popchatSelectionClicked((char*)json.UTF8String);
}
- (void)stop {
 [self.permissionTimer invalidate];self.permissionTimer=nil;
 [self hide];if(self.globalMonitor)[NSEvent removeMonitor:self.globalMonitor];if(self.localMonitor)[NSEvent removeMonitor:self.localMonitor];
 if(self.workspaceMonitor)[NSWorkspace.sharedWorkspace.notificationCenter removeObserver:self.workspaceMonitor];
 self.globalMonitor=nil;self.localMonitor=nil;self.workspaceMonitor=nil;
}
@end
static PopchatSelection *selection;
void popchatSelectionConfigure(const char *json){
 if(!selection)selection=[PopchatSelection new];[selection hide];
 NSData *data=[[NSString stringWithUTF8String:json] dataUsingEncoding:NSUTF8StringEncoding];selection.config=[NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
 [selection refreshMonitor];
}
int popchatSelectionPermission(int prompt) {
 BOOL trusted = AXIsProcessTrusted();
 if (prompt && !trusted) {
  // TCC may suppress repeat prompts. Always provide the actual settings page.
  dispatch_async(dispatch_get_main_queue(), ^{
   AXIsProcessTrustedWithOptions((CFDictionaryRef)@{(id)kAXTrustedCheckOptionPrompt:@YES});
   [NSWorkspace.sharedWorkspace openURL:[NSURL URLWithString:
       @"x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"]];
  });
 }
 return trusted;
}
void popchatSelectionStop(void){dispatch_async(dispatch_get_main_queue(),^{[selection stop];});}
