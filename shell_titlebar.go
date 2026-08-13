package shell

// TitleBarOptions controls native title-bar presentation.
type TitleBarOptions struct {
	// Overlay lets web content occupy the title-bar area and enables regions
	// marked with "--shell-window-drag: drag" to move the window.
	Overlay bool

	// ControlsOffset moves the macOS traffic lights inward (positive X) and
	// down (positive Y), in logical pixels, when Overlay is enabled.
	ControlsOffsetX int
	ControlsOffsetY int
}
