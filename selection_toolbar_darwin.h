#import <AppKit/AppKit.h>

// Shared by the live nonactivating panel and the visual preview.
NSView *popchatSelectionToolbar(NSArray<NSDictionary *> *buttons,
                               CGFloat maxWidth, id target, SEL action);
