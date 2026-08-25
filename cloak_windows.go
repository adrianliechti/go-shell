//go:build windows

package shell

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

// go-webview2 creates its window and calls ShowWindow before it embeds the
// WebView2 controller, and the shell then restores the saved placement and
// replaces the caption — all of it already on screen. The user sees a blank
// white window that only later jumps to its saved position and grows its
// custom frame once content arrives.
//
// So the window starts out cloaked: DWM keeps it off screen while it stays an
// ordinary activatable window that lays out and paints, which SW_HIDE would
// not — Chromium treats a hidden host as occluded and stops producing frames,
// so the reveal would wait for a frame that never comes.
//
// Cloaking has to happen before go-webview2's ShowWindow, and there is no seam
// between its CreateWindowEx and that call, so a thread-local WH_CBT hook
// catches the window the moment it is created.

var (
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetClassName        = user32.NewProc("GetClassNameW")
)

const (
	whCBT         = 5
	hcbtCreateWnd = 3

	dwmwaCloak = 13

	wmNCCreate = 0x0081

	// webViewClass is the window class go-webview2 registers for its window.
	webViewClass = "webview"

	// revealTimeout uncloaks the window even if the page never signals, so a
	// navigation that fails outright cannot leave an invisible window behind.
	revealTimeout = 3 * time.Second

	// revealProbeInterval closes the race between WebView2's asynchronous
	// document-script registration and the initial navigation. Evaluating the
	// same idempotent script against the live top-level document gives it a
	// second path to signal without delaying navigation.
	revealProbeInterval = 50 * time.Millisecond
)

// The page signals after its first rendering opportunity. The zero-delay timer
// runs in a task after requestAnimationFrame's rendering update has completed;
// the longer timer is a fail-open path for a cloaked window whose animation
// frames are throttled. Initialization scripts also run in child frames, so
// only the top-level document may reveal the host window.
const revealScript = `(function () {
 if (window !== window.top || location.href === 'about:blank') {
  return;
 }

 const key = Symbol.for('github.com/adrianliechti/go-shell/reveal');
 if (window[key]) {
  return;
 }

 window[key] = true;

 function reveal() {
  if (typeof window.__shellReveal === 'function') {
   window.__shellReveal();
   return;
  }

  // Bind registers the native callback synchronously but installs its JS
  // wrapper asynchronously. Call the same RPC protocol directly when this
  // runtime probe reaches the document before that wrapper does.
  const id = -1;
  const rpc = window._rpc = window._rpc || {nextSeq: 1};
  rpc[id] = {
   resolve: function () { delete rpc[id]; },
   reject: function () { delete rpc[id]; }
  };
  window.chrome.webview.postMessage(JSON.stringify({
   id: id,
   method: '__shellReveal',
   params: []
  }));
 }

 function schedule() {
  requestAnimationFrame(function () {
   setTimeout(reveal, 0);
  });
  setTimeout(reveal, 100);
 }

 if (document.readyState === 'loading') {
  addEventListener('DOMContentLoaded', schedule);
 } else {
  schedule();
 }
})();`

var (
	cloakMu       sync.Mutex
	cloakPending  bool
	cloakedWindow uintptr
	cloakOriginal uintptr

	cbtCallback     uintptr
	cbtCallbackOnce sync.Once

	cloakProcCallback uintptr
	cloakProcOnce     sync.Once
)

// cloakNextWindow arms the hook that cloaks the web view window as it is
// created. The returned function removes the hook and reports the window that
// was cloaked, or 0 if none was caught — in which case the window is simply
// visible from the start, as it was before.
func cloakNextWindow() func() uintptr {
	cbtCallbackOnce.Do(func() {
		cbtCallback = windows.NewCallback(cbtProc)
	})

	cloakMu.Lock()
	cloakPending = true
	cloakedWindow = 0
	cloakOriginal = 0
	cloakMu.Unlock()

	hook, _, _ := procSetWindowsHookEx.Call(whCBT, cbtCallback, 0, uintptr(windows.GetCurrentThreadId()))

	return func() uintptr {
		if hook != 0 {
			procUnhookWindowsHookEx.Call(hook)
		}

		cloakMu.Lock()
		defer cloakMu.Unlock()

		cloakPending = false

		return cloakedWindow
	}
}

// DWM rejects the handle at HCBT_CREATEWND (E_HANDLE) — the window is not yet
// known to the compositor. The window procedure is swapped there instead, so
// the cloak can be applied from WM_NCCREATE, the first message the window
// receives and still inside CreateWindowEx, well before ShowWindow.
func cbtProc(code, wparam, lparam uintptr) uintptr {
	if code == hcbtCreateWnd {
		cloakMu.Lock()

		if cloakPending && isWebViewWindow(wparam, lparam) {
			cloakPending = false
			cloakedWindow = wparam

			cloakProcOnce.Do(func() {
				cloakProcCallback = windows.NewCallback(cloakProc)
			})

			cloakOriginal, _, _ = procSetWindowLongPtr.Call(wparam, gwlpWndProc, cloakProcCallback)
		}

		cloakMu.Unlock()
	}

	next, _, _ := procCallNextHookEx.Call(0, code, wparam, lparam)

	return next
}

// cloakProc cloaks the window on its first message and immediately restores the
// original procedure, so go-webview2 (and the frame installed later) own the
// window procedure as they normally would.
func cloakProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	cloakMu.Lock()
	original := cloakOriginal

	if original != 0 {
		procSetWindowLongPtr.Call(hwnd, gwlpWndProc, original)
		cloakOriginal = 0

		// A window DWM refuses to cloak stays visible; forget it so the reveal
		// does not later toggle an attribute that was never set.
		if setCloaked(hwnd, true) != 0 {
			cloakedWindow = 0
		}
	}

	cloakMu.Unlock()

	if original == 0 {
		result, _, _ := procDefWindowProc.Call(hwnd, msg, wparam, lparam)
		return result
	}

	result, _, _ := procCallWindowProc.Call(original, hwnd, msg, wparam, lparam)

	return result
}

// isWebViewWindow reports whether the window being created is the top-level
// web view window rather than one of the child windows WebView2 creates while
// it embeds itself.
func isWebViewWindow(hwnd, lparam uintptr) bool {
	if lparam != 0 {
		if info := (*cbtCreateWnd)(unsafe.Pointer(lparam)); info.CreateStruct != nil && info.CreateStruct.Parent != 0 {
			return false
		}
	}

	return windowClass(hwnd) == webViewClass
}

func windowClass(hwnd uintptr) string {
	buf := make([]uint16, 256)

	n, _, _ := procGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))

	if n == 0 {
		return ""
	}

	return windows.UTF16ToString(buf[:n])
}

// windowReveal owns the cloak from successful window creation until either the
// page signals or run returns. Keeping cleanup alive for the entire setup path
// prevents an error (for example while installing the custom frame) from
// stranding a cloaked native window.
type windowReveal struct {
	hwnd uintptr
	once sync.Once
	done atomic.Bool

	timerMu      sync.Mutex
	timeoutTimer *time.Timer
	probeTimer   *time.Timer
}

func newWindowReveal(hwnd uintptr) *windowReveal {
	return &windowReveal{hwnd: hwnd}
}

// arm starts the fail-open timeout immediately before navigation. Starting it
// any earlier could consume the timeout while native window setup is still in
// progress.
func (r *windowReveal) arm(w webview2.WebView) {
	if r.hwnd == 0 || r.done.Load() {
		return
	}

	r.timerMu.Lock()
	r.timeoutTimer = time.AfterFunc(revealTimeout, func() {
		w.Dispatch(r.reveal)
	})
	r.timerMu.Unlock()

	r.scheduleProbe(w)
}

func (r *windowReveal) scheduleProbe(w webview2.WebView) {
	r.timerMu.Lock()
	defer r.timerMu.Unlock()

	if r.done.Load() {
		return
	}

	r.probeTimer = time.AfterFunc(revealProbeInterval, func() {
		w.Dispatch(func() {
			if r.done.Load() {
				return
			}

			w.Eval(revealScript)
			r.scheduleProbe(w)
		})
	})
}

func (r *windowReveal) reveal() {
	r.once.Do(func() {
		r.done.Store(true)

		if r.hwnd != 0 {
			setCloaked(r.hwnd, false)
		}
	})

	r.timerMu.Lock()
	if r.timeoutTimer != nil {
		r.timeoutTimer.Stop()
		r.timeoutTimer = nil
	}

	if r.probeTimer != nil {
		r.probeTimer.Stop()
		r.probeTimer = nil
	}
	r.timerMu.Unlock()
}

func (r *windowReveal) close() {
	r.reveal()
}

func setCloaked(hwnd uintptr, cloaked bool) uintptr {
	value := int32(0)

	if cloaked {
		value = 1
	}

	rc, _, _ := procSetWindowAttribute.Call(hwnd, dwmwaCloak, uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value))

	return rc
}

type cbtCreateWnd struct {
	CreateStruct    *createStruct
	HWNDInsertAfter uintptr
}

type createStruct struct {
	CreateParams uintptr
	Instance     windows.Handle
	Menu         uintptr
	Parent       uintptr
	CY           int32
	CX           int32
	Y            int32
	X            int32
	Style        int32
	Name         *uint16
	Class        *uint16
	ExStyle      uint32
}
