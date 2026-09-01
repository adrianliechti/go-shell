//go:build windows

package shell

import "testing"

func TestOverlayButtonRects(t *testing.T) {
	o := &overlay{
		buttons: []button{buttonMinimize, buttonMaximize, buttonClose},
		height:  40,
		dpi:     96,
	}

	tests := []struct {
		button button
		left   int32
		right  int32
	}{
		{buttonMinimize, 0, 46},
		{buttonMaximize, 46, 92},
		{buttonClose, 92, 138},
	}

	for _, test := range tests {
		r, ok := o.rectOf(test.button)

		if !ok {
			t.Fatalf("rectOf(%v) returned no rectangle", test.button)
		}

		if r.Left != test.left || r.Right != test.right || r.Top != 0 || r.Bottom != 40 {
			t.Fatalf("rectOf(%v) = %+v", test.button, r)
		}
	}
}

func TestOverlayControlWidthScales(t *testing.T) {
	o := &overlay{
		buttons: []button{buttonMinimize, buttonMaximize, buttonClose},
		dpi:     144,
	}

	if got, want := o.width(), int32(207); got != want {
		t.Fatalf("control width = %d, want %d", got, want)
	}
}

func TestOverlayButtonRectsScale(t *testing.T) {
	o := &overlay{
		buttons: []button{buttonMinimize, buttonMaximize, buttonClose},
		height:  60,
		dpi:     144,
	}

	tests := []struct {
		button button
		left   int32
		right  int32
	}{
		{buttonMinimize, 0, 69},
		{buttonMaximize, 69, 138},
		{buttonClose, 138, 207},
	}

	for _, test := range tests {
		r, ok := o.rectOf(test.button)
		if !ok {
			t.Fatalf("rectOf(%v) returned no rectangle", test.button)
		}

		if r.Left != test.left || r.Right != test.right || r.Top != 0 || r.Bottom != 60 {
			t.Errorf("rectOf(%v) = %+v", test.button, r)
		}
	}
}
