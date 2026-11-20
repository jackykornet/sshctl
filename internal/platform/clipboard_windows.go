package platform

import (
	"runtime"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	user32                     = syscall.NewLazyDLL("user32.dll")
	kernel32                   = syscall.NewLazyDLL("kernel32.dll")
	isClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	openClipboard              = user32.NewProc("OpenClipboard")
	closeClipboard             = user32.NewProc("CloseClipboard")
	getClipboardData           = user32.NewProc("GetClipboardData")
	globalLock                 = kernel32.NewProc("GlobalLock")
	globalUnlock               = kernel32.NewProc("GlobalUnlock")
)

const cfUnicodeText = 13

// readClipboard reads CF_UNICODETEXT with the Win32 clipboard API.
func readClipboard() (string, error) {
	// The clipboard is opened for the calling thread, which must close it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if ok, _, _ := isClipboardFormatAvailable.Call(cfUnicodeText); ok == 0 {
		return "", nil
	}
	// Another program may hold the clipboard open for a moment.
	var err error
	for range 10 {
		var ok uintptr
		if ok, _, err = openClipboard.Call(0); ok != 0 {
			err = nil
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		return "", err
	}
	defer closeClipboard.Call()
	h, _, err := getClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", err
	}
	p, _, err := globalLock.Call(h)
	if p == 0 {
		return "", err
	}
	defer globalUnlock.Call(h)
	// p points at the clipboard's NUL-terminated UTF-16 text, which stays
	// valid until GlobalUnlock; the string is a copy of it. The memory is
	// Windows', not Go's, so turning the uintptr into a pointer is safe; it
	// goes through &p because vet cannot tell that.
	text := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	n := 0
	for *(*uint16)(unsafe.Add(text, 2*n)) != 0 {
		n++
	}
	return string(utf16.Decode(unsafe.Slice((*uint16)(text), n))), nil
}
