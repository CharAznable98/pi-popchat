//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework UserNotifications
#import <UserNotifications/UserNotifications.h>
#include <stdlib.h>
extern void popchatNotificationClicked(char *sid);
static int popchatProbeDelivered(const char *sid) {
 @autoreleasepool {
  NSString *identifier=[NSString stringWithFormat:@"popchat-%s",sid];
  dispatch_semaphore_t sem=dispatch_semaphore_create(0);
  __block int found=0;
  [[UNUserNotificationCenter currentNotificationCenter] getDeliveredNotificationsWithCompletionHandler:^(NSArray<UNNotification *> *items){
   for(UNNotification *item in items) if([item.request.identifier isEqualToString:identifier]) found=1;
   dispatch_semaphore_signal(sem);
  }];
  long timedout=dispatch_semaphore_wait(sem,dispatch_time(DISPATCH_TIME_NOW,3*NSEC_PER_SEC));
  if(!timedout) dispatch_release(sem);
  return timedout ? -1 : found;
 }
}
static void popchatProbeRemoveNotification(const char *sid) {
 @autoreleasepool {[[UNUserNotificationCenter currentNotificationCenter] removeDeliveredNotificationsWithIdentifiers:@[[NSString stringWithFormat:@"popchat-%s",sid]]];}
}
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"os"
	"time"
	"unsafe"
)

func runNotificationProbe(d *Desktop) int {
	sid, err := d.engine.NewSession("panel", "")
	if err != nil {
		return 1
	}
	_, _ = d.engine.NewSession("main", "")
	d.hidePanel()
	d.hideMain()
	d.notify(sid, "通知验收", "Pi Popchat 系统通知验收")
	cs := C.CString(sid)
	defer C.free(unsafe.Pointer(cs))
	delivered := false
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if C.popchatProbeDelivered(cs) == 1 {
			delivered = true
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	noFocus := !d.main.IsVisible() && !d.panel.IsVisible()
	// Exercise the same native callback boundary used by the system delegate.
	C.popchatNotificationClicked(cs)
	routed := false
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if d.engine.CurrentID("main") == sid && d.main.IsVisible() {
			routed = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	C.popchatProbeRemoveNotification(cs)
	result := map[string]any{"suite": "system-notification", "systemDelivered": delivered, "noFocusSteal": noFocus, "nativeCallbackTargetsSession": routed, "physicalNotificationClickObserved": false}
	b, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(b))
	if path := os.Getenv("PI_POPCHAT_CHECK_OUTPUT"); path != "" {
		_ = os.WriteFile(path, b, 0600)
	}
	if delivered && noFocus && routed {
		return 0
	}
	return 1
}
