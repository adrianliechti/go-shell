//go:build windows

package shell

import "testing"

func TestOverlayButtonRects(t *testing.T) {
	o := &overlay{
		buttons: []button{buttonClose},
		height:  40,
		dpi:     96,
	}

	tests := []struct {
		button button
		left   int32
		right  int32
	}{
		{buttonClose, 0, 34},
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

func TestOverlayMenuWidthScales(t *testing.T) {
	o := &overlay{
		buttons: []button{buttonMenu},
		dpi:     144,
	}

	if got, want := o.width(), int32(60); got != want {
		t.Fatalf("menu width = %d, want %d", got, want)
	}
}
