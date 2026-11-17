package shell

import "testing"

func TestShutdownRejectsPendingAndNewWindows(t *testing.T) {
	windowQueue.Lock()
	windowQueue.startURL = "http://localhost/"
	windowQueue.Unlock()
	t.Cleanup(func() { shellDrainWindowRequests(1) })

	// No AppKit event loop is running. Shutdown must finish both requests.
	var results []<-chan error
	for range 2 {
		result, err := enqueueWindow(nil)
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, result)
	}

	shellDrainWindowRequests(1)
	for _, result := range results {
		select {
		case err := <-result:
			if err != errWindowNotRunning {
				t.Errorf("queued window returned %v, want %v", err, errWindowNotRunning)
			}
		default:
			t.Fatal("shutdown left a window request pending")
		}
	}
	if _, err := enqueueWindow(nil); err != errWindowNotRunning {
		t.Errorf("new window during shutdown returned %v, want %v", err, errWindowNotRunning)
	}
}
