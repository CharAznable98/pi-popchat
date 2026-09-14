// Read-only check of an ALREADY OPEN manual fixture window. Never navigates,
// activates applications, synthesizes input, reads clipboard or grants access.
// No text is emitted; exact fixture matches and API metadata only.
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>

static id value(AXUIElementRef element, CFStringRef attribute) {
 CFTypeRef result=NULL;
 if(AXUIElementCopyAttributeValue(element,attribute,&result)!=kAXErrorSuccess)return nil;
 return CFBridgingRelease(result);
}
int main(void) {
 @autoreleasepool {
  NSMutableArray *observations=[NSMutableArray new];BOOL found=NO,matched=NO;
  for(NSRunningApplication *application in [NSRunningApplication runningApplicationsWithBundleIdentifier:@"com.google.Chrome"]){
   AXUIElementRef app=AXUIElementCreateApplication(application.processIdentifier);AXUIElementSetMessagingTimeout(app,.3);
   NSArray *windows=value(app,kAXWindowsAttribute);CFRelease(app);
   if(![windows isKindOfClass:NSArray.class])continue;
   for(id w in windows){
    AXUIElementRef window=(__bridge AXUIElementRef)w;
    NSString *title=value(window,kAXTitleAttribute);
    if(![title isKindOfClass:NSString.class] || ![title containsString:@"划词验收：独立选区复测"])continue;
    found=YES;NSMutableArray *queue=[NSMutableArray arrayWithObject:w];NSUInteger index=0;
    while(index<queue.count && index<500){
     AXUIElementRef e=(__bridge AXUIElementRef)queue[index++];NSString *role=value(e,kAXRoleAttribute);
     if([role isEqual:@"AXWebArea"] || [role isEqual:@"AXTextArea"]){
      NSString *text=value(e,kAXSelectedTextAttribute);NSString *api=@"standard";
      if(![text isKindOfClass:NSString.class] || !text.length){
       id marker=value(e,CFSTR("AXSelectedTextMarkerRange"));CFTypeRef selected=NULL;
       if(marker && AXUIElementCopyParameterizedAttributeValue(e,CFSTR("AXStringForTextMarkerRange"),(__bridge CFTypeRef)marker,&selected)==kAXErrorSuccess && selected){text=CFBridgingRelease(selected);api=@"text-marker";}
      }
      BOOL valid=[text isKindOfClass:NSString.class];BOOL exact=valid&&[text isEqual:@"Synthetic selection"];
      matched=matched||exact;
      [observations addObject:@{@"role":role,@"api":api,@"selectedLength":@(valid?text.length:0),@"matchesFixtureEnglish":@(exact)}];
     }
     NSArray *children=value(e,kAXChildrenAttribute);
     if([children isKindOfClass:NSArray.class])for(id child in children)if(CFGetTypeID((__bridge CFTypeRef)child)==AXUIElementGetTypeID()&&queue.count<1000)[queue addObject:child];
    }
   }
  }
  NSDictionary *report=@{@"fixtureWindowFound":@(found),@"exactSelectionRead":@(matched),@"accessibilityTrusted":@(AXIsProcessTrusted()),@"observations":observations};
  NSData *data=[NSJSONSerialization dataWithJSONObject:report options:NSJSONWritingPrettyPrinted error:nil];fwrite(data.bytes,1,data.length,stdout);puts("");
  return matched?0:1;
 }
}
