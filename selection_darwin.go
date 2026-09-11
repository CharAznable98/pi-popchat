//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework AppKit -framework ApplicationServices -framework NaturalLanguage
#include <stdlib.h>
void popchatSelectionConfigure(const char *json);
int popchatSelectionPermission(int prompt);
void popchatSelectionStop(void);
*/
import "C"
import (
	"encoding/json"
	"github.com/wailsapp/wails/v3/pkg/application"
	"pi-popchat/internal/core"
	"sync"
	"time"
	"unsafe"
)

var selectionMu sync.RWMutex
var selectionDesktop *Desktop

func selectionPermission(prompt bool) bool {
	p := C.int(0)
	if prompt {
		p = 1
	}
	return C.popchatSelectionPermission(p) != 0
}
func (d *Desktop) configureSelection() {
	settings := d.engine.Settings().Selection
	if settings == nil {
		settings = core.DefaultSelectionSettings()
	}
	type button struct {
		core.SelectionButton
		DefaultTranslation bool `json:"defaultTranslation"`
	}
	buttons := []button{}
	for _, b := range settings.Buttons {
		buttons = append(buttons, button{b, b.ID == "translate" && b.Template == core.DefaultSelectionSettings().Buttons[0].Template})
	}
	data, _ := json.Marshal(map[string]any{"enabled": settings.Enabled, "buttons": buttons})
	selectionMu.Lock()
	selectionDesktop = d
	selectionMu.Unlock()
	c := C.CString(string(data))
	defer C.free(unsafe.Pointer(c))
	application.InvokeSync(func() { C.popchatSelectionConfigure(c) })
}
func stopSelection() {
	selectionMu.Lock()
	selectionDesktop = nil
	selectionMu.Unlock()
	C.popchatSelectionStop()
}

//export popchatSelectionClicked
func popchatSelectionClicked(payload *C.char) {
	var v struct {
		Text           string `json:"text"`
		Template       string `json:"template"`
		Language       string `json:"language"`
		Time           int64  `json:"time"`
		Timezone       string `json:"timezone"`
		TimezoneOffset int    `json:"timezoneOffset"`
	}
	if json.Unmarshal([]byte(C.GoString(payload)), &v) != nil {
		return
	}
	selectionMu.RLock()
	d := selectionDesktop
	selectionMu.RUnlock()
	if d == nil {
		return
	}
	// Capture placement before showing or activating any conversation window.
	screen := mouseScreen()
	go func() {
		location, err := time.LoadLocation(v.Timezone)
		if err != nil {
			location = time.FixedZone(v.Timezone, v.TimezoneOffset)
		}
		prompt := core.RenderSelection(v.Template, v.Text, v.Language, time.UnixMilli(v.Time).In(location))
		if _, err = d.engine.SubmitSelection(prompt); err != nil {
			d.setError(err.Error())
			return
		}
		d.presentPanel(screen)
	}()
}

//export popchatSelectionPermissionChanged
func popchatSelectionPermissionChanged() {
	selectionMu.RLock()
	d := selectionDesktop
	selectionMu.RUnlock()
	if d != nil {
		go d.app.Event.Emit("popchat:changed")
	}
}
