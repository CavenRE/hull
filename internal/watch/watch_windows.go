//go:build windows

package watch

import (
	"encoding/binary"
	"errors"
	"path/filepath"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// changeBuffer is what one ReadDirectoryChangesW call can fill. A burst that
// outgrows it (a git checkout) makes Windows report an overflow, which the
// watcher answers with a full reset.
const changeBuffer = 1 << 20

const changeMask = windows.FILE_NOTIFY_CHANGE_FILE_NAME | windows.FILE_NOTIFY_CHANGE_DIR_NAME |
	windows.FILE_NOTIFY_CHANGE_LAST_WRITE | windows.FILE_NOTIFY_CHANGE_SIZE

// startBackend watches the whole tree through one directory handle: Windows
// reports changes in every folder below it, so there is no per-folder watch to
// set up, keep up to date, or run out of.
func startBackend(w *Watcher) (func(), error) {
	name, err := windows.UTF16PtrFromString(w.dir)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, err
	}
	signal, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	quit, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(signal)
		_ = windows.CloseHandle(h)
		return nil, err
	}
	closeAll := func() {
		_ = windows.CloseHandle(quit)
		_ = windows.CloseHandle(signal)
		_ = windows.CloseHandle(h)
	}
	buf := make([]byte, changeBuffer)
	ov := new(windows.Overlapped)
	read := func() error {
		*ov = windows.Overlapped{HEvent: signal}
		_ = windows.ResetEvent(signal)
		err := windows.ReadDirectoryChanges(h, &buf[0], uint32(len(buf)), true, changeMask, nil, ov, 0)
		if errors.Is(err, windows.ERROR_IO_PENDING) {
			return nil
		}
		return err
	}
	// A filesystem that cannot report changes (a WSL distro reached over UNC
	// answers "Incorrect function") fails this first request at once.
	if err := read(); err != nil {
		closeAll()
		return nil, err
	}

	// Only the closer closes the handles, after the goroutine is gone, so a
	// backend that stops on its own never leaves Close signalling a handle
	// value Windows may have handed out again.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			r, err := windows.WaitForMultipleObjects([]windows.Handle{signal, quit}, false, windows.INFINITE)
			if err != nil || r != windows.WAIT_OBJECT_0 {
				// The read is still pending: cancel it and wait until Windows is
				// done with buf before the handles close.
				_ = windows.CancelIoEx(h, ov)
				var n uint32
				_ = windows.GetOverlappedResult(h, ov, &n, true)
				return
			}
			var n uint32
			err = windows.GetOverlappedResult(h, ov, &n, false)
			switch {
			case err == nil && n > 0:
				if !parseChanges(w, buf[:n]) {
					return
				}
			case err == nil || errors.Is(err, windows.ERROR_NOTIFY_ENUM_DIR):
				if !w.emit(event{overflow: true}) {
					return
				}
			default:
				w.fail(err)
				return
			}
			if err := read(); err != nil {
				w.fail(err)
				return
			}
		}
	}()
	return func() {
		_ = windows.SetEvent(quit)
		<-done
		closeAll()
	}, nil
}

// parseChanges turns a buffer of FILE_NOTIFY_INFORMATION records into events.
// It reports false once the watcher is closing.
func parseChanges(w *Watcher, b []byte) bool {
	for off := 0; off+12 <= len(b); {
		next := int(binary.LittleEndian.Uint32(b[off:]))
		action := binary.LittleEndian.Uint32(b[off+4:])
		size := int(binary.LittleEndian.Uint32(b[off+8:]))
		if off+12+size > len(b) {
			return true
		}
		name := make([]uint16, size/2)
		for i := range name {
			name[i] = binary.LittleEndian.Uint16(b[off+12+2*i:])
		}
		ev := event{path: filepath.Join(w.dir, string(utf16.Decode(name)))}
		switch action {
		case windows.FILE_ACTION_ADDED:
			ev.op = opCreate
		case windows.FILE_ACTION_RENAMED_NEW_NAME:
			ev.op = opRenamedIn
		case windows.FILE_ACTION_REMOVED, windows.FILE_ACTION_RENAMED_OLD_NAME:
			ev.op = opGone
		default:
			ev.op = opWrite
		}
		if !w.emit(ev) {
			return false
		}
		if next == 0 {
			return true
		}
		off += next
	}
	return true
}
