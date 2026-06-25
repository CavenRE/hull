//go:build windows && installer

package main

import "syscall"

// Minimal user32 calls to make the WebView2 window frameless and draggable —
// matching the app's decorationless window with custom controls.
var (
	user32             = syscall.NewLazyDLL("user32.dll")
	procGetWindowLong  = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLong  = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos   = user32.NewProc("SetWindowPos")
	procShowWindow     = user32.NewProc("ShowWindow")
	procReleaseCapture   = user32.NewProc("ReleaseCapture")
	procSendMessage      = user32.NewProc("SendMessageW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

const (
	smCXScreen = 0
	smCYScreen = 1
)

const (
	gwlStyle        = ^uintptr(15) // -16 (GWL_STYLE)
	wsCaption       = 0x00C00000   // title bar
	wsThickFrame    = 0x00040000   // resize border
	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoZorder     = 0x0004
	swpFrameChanged = 0x0020
	swMinimize      = 6
	wmNCLButtonDown = 0x00A1
	htCaption       = 2
)

// makeFrameless removes the native title bar and resize border from hwnd,
// leaving just the WebView2 content (the HTML draws its own controls).
func makeFrameless(hwnd uintptr) {
	style, _, _ := procGetWindowLong.Call(hwnd, gwlStyle)
	style &^= wsCaption | wsThickFrame
	procSetWindowLong.Call(hwnd, gwlStyle, style)
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpNoSize|swpNoMove|swpNoZorder|swpFrameChanged)
}

func minimizeWindow(hwnd uintptr) { procShowWindow.Call(hwnd, swMinimize) }

// resizeAndCenter sizes the window to w×h and re-centers it on screen, so the
// frameless window always hugs its content.
func resizeAndCenter(hwnd uintptr, w, h int) {
	sw, _, _ := procGetSystemMetrics.Call(smCXScreen)
	sh, _, _ := procGetSystemMetrics.Call(smCYScreen)
	x := (int(sw) - w) / 2
	y := (int(sh) - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoZorder)
}

// startWindowDrag kicks off the standard borderless move loop (so the user can
// drag the window by the custom title region).
func startWindowDrag(hwnd uintptr) {
	procReleaseCapture.Call()
	procSendMessage.Call(hwnd, wmNCLButtonDown, htCaption, 0)
}
