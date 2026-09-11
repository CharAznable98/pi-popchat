// Synthetic fixture: show production toolbar without enabling selection capture.
#import "../selection_darwin.m"
void popchatSelectionClicked(char *payload) {}
void popchatSelectionPermissionChanged(void) {}
int main(int argc, const char **argv) { @autoreleasepool {
 [NSApplication sharedApplication];
 popchatSelectionConfigure("{\"enabled\":false,\"buttons\":[{\"id\":\"translate\",\"name\":\"翻译\"},{\"id\":\"explain\",\"name\":\"解释\"}]}");
 dispatch_async(dispatch_get_main_queue(), ^{
 selection.captured=@{@"text":@"synthetic"};
 [selection showAt:NSMakePoint(300,400)];
 if (NSHeight(selection.panel.frame) != 46) { fprintf(stderr,"FAIL: toolbar height clipped by previous window size\n"); exit(1); }
 [[NSString stringWithFormat:@"%ld",(long)selection.panel.windowNumber] writeToFile:[NSString stringWithUTF8String:argv[1]] atomically:YES encoding:NSUTF8StringEncoding error:nil];
 });
 [NSApp run];
} return 0; }
