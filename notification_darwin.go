//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework AppKit -framework UserNotifications
#include <stdlib.h>
void popchatSetupNotifications(void);
void popchatSendNotification(const char *sid, const char *title, const char *body);
*/
import "C"
import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"log"
	"sync"
	"unsafe"
)

var notificationMu sync.RWMutex
var notificationClick func(string)
var notificationError func(string)

func setupNotifications(click func(string), onError func(string)) {
	notificationMu.Lock()
	notificationClick = click
	notificationError = onError
	notificationMu.Unlock()
	application.InvokeSync(func() { C.popchatSetupNotifications() })
}
func sendNotification(sid, title, body string) {
	s, t, b := C.CString(sid), C.CString(title), C.CString(body)
	defer C.free(unsafe.Pointer(s))
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(b))
	C.popchatSendNotification(s, t, b)
}

//export popchatNotificationClicked
func popchatNotificationClicked(sid *C.char) {
	id := C.GoString(sid)
	notificationMu.RLock()
	fn := notificationClick
	notificationMu.RUnlock()
	if fn != nil {
		go fn(id)
	}
}

//export popchatNotificationError
func popchatNotificationError(message *C.char) {
	msg := C.GoString(message)
	log.Printf("notification: %s", msg)
	notificationMu.RLock()
	fn := notificationError
	notificationMu.RUnlock()
	if fn != nil {
		go fn("系统通知不可用。可在 macOS 系统设置 → 通知 → Pi Popchat 中启用。")
	}
}
func detachNotifications() {
	notificationMu.Lock()
	notificationClick = nil
	notificationError = nil
	notificationMu.Unlock()
}
