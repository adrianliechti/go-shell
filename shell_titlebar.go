package shell

// TitleBarOptions controls native title-bar presentation.
type TitleBarOptions struct {
	// Overlay lets web content occupy the title-bar area and enables regions
	// marked with "--shell-window-drag: drag" to move the window.
	Overlay bool

	// ControlsOffset moves the macOS traffic lights inward (positive X) and
	// down (positive Y), in logical pixels, when Overlay is enabled. Windows
	// keeps its caption controls at the system-defined trailing edge.
	ControlsOffsetX int
	ControlsOffsetY int

	// Height is the height of the title-bar strip in logical pixels. It is
	// published through window.shell and as --shell-titlebar-height. Windows
	// also uses it to lay out the caption controls; zero uses the platform
	// title-bar height.
	Height int
}
