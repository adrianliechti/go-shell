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

	// Height is the height of the title-bar strip in logical pixels. Windows uses
	// it to lay out the window controls the shell draws into the overlay and
	// defaults to the system caption height; macOS ignores it.
	Height int

	// Menu draws a menu button at the leading edge of the Windows overlay that
	// opens FileMenu as a popup — the counterpart of the macOS menu bar, which a
	// window without a native caption has no room for.
	Menu bool
}
