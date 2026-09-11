// Regression: dispatch ordinary input through the production local monitor.
// No synthetic OS input is posted and no user window or Agent is opened.
#import <AppKit/AppKit.h>
extern void popchatSelectionConfigure(const char *json);
extern void popchatSelectionStop(void);
void popchatSelectionClicked(char *payload) {}
void popchatSelectionPermissionChanged(void) {}

int main(void) {
 @autoreleasepool {
  [NSApplication sharedApplication];
  popchatSelectionConfigure("{\"enabled\":true,\"buttons\":[]}");
  NSEventType types[] = {NSEventTypeLeftMouseDown, NSEventTypeLeftMouseUp,
                        NSEventTypeRightMouseDown};
  for (NSUInteger i = 0; i < sizeof(types) / sizeof(types[0]); i++) {
   NSEvent *event = [NSEvent mouseEventWithType:types[i]
      location:NSZeroPoint modifierFlags:0 timestamp:0 windowNumber:0
      context:nil eventNumber:1 clickCount:1 pressure:1];
   @try {
    [NSApp sendEvent:event];
   } @catch (NSException *error) {
    fprintf(stderr, "FAIL mouse event %lu: %s: %s\n", (unsigned long)types[i],
            error.name.UTF8String, error.reason.UTF8String);
    return 1;
   }
  }
  NSEvent *escape = [NSEvent keyEventWithType:NSEventTypeKeyDown
      location:NSZeroPoint modifierFlags:0 timestamp:0 windowNumber:0
      context:nil characters:@"\033" charactersIgnoringModifiers:@"\033"
      isARepeat:NO keyCode:53];
  @try {
   [NSApp sendEvent:escape];
  } @catch (NSException *error) {
   fprintf(stderr, "FAIL Escape: %s\n", error.reason.UTF8String);
   return 1;
  }
  popchatSelectionStop();
  puts("PASS: production selection monitor accepts mouse down/up/right-click and Escape");
 }
 return 0;
}
