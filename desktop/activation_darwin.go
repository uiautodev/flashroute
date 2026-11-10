//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

static void setRegularActivationPolicy(void) {
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
}

static void setAccessoryActivationPolicy(void) {
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// setDockActivationPolicy decides whether the app takes part in the Dock and
// the Cmd+Tab switcher. A regular app has both, an accessory app only has the
// menu bar item. AppKit requires the switch to run on the main thread.
func setDockActivationPolicy(regular bool) {
	application.InvokeSync(func() {
		if regular {
			C.setRegularActivationPolicy()
			return
		}
		C.setAccessoryActivationPolicy()
	})
}
