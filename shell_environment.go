package shell

import "fmt"

// shellEnvironmentScript installs the small, read-only JavaScript surface that
// describes the native host. It is injected at document start on every page;
// title-bar geometry is filled in asynchronously by the platform implementation.
func shellEnvironmentScript(platform string, overlay bool) string {
	return fmt.Sprintf(`(() => {
	const platform = %q;
	const overlay = %t;
	const state = { height: 0, left: 0, right: 0, maximized: false, ready: false };
	const insets = Object.freeze(Object.defineProperties({}, {
		left: { enumerable: true, get: () => state.left },
		right: { enumerable: true, get: () => state.right },
	}));
	const titleBar = Object.freeze(Object.defineProperties({ overlay, insets }, {
		height: { enumerable: true, get: () => state.height },
		maximized: { enumerable: true, get: () => state.maximized },
	}));
	Object.defineProperty(window, 'shell', {
		value: Object.freeze({ platform, titleBar }),
		enumerable: true,
	});

	const syncDocument = () => {
		const el = document.documentElement;
		if (!el || !overlay) return;
		el.dataset.windowChrome = platform + '-overlay';
		if (!state.ready) return;
		const style = el.style;
		style.setProperty('--shell-titlebar-height', state.height + 'px');
		style.setProperty('--shell-titlebar-inset-left', state.left + 'px');
		style.setProperty('--shell-titlebar-inset-right', state.right + 'px');
		el.dataset.shellWindow = state.maximized ? 'maximized' : 'normal';
	};
	const applyTitleBarInsets = (next) => {
		if (!next || !overlay) return;
		state.height = next.height;
		state.left = next.left;
		state.right = next.right;
		state.maximized = next.maximized;
		state.ready = true;
		syncDocument();
		window.dispatchEvent(new CustomEvent('shell:titlebar-change', { detail: {
			overlay,
			height: state.height,
			insets: { left: state.left, right: state.right },
			maximized: state.maximized,
		} }));
	};
	Object.defineProperty(window, '__shellApplyTitleBarInsets', {
		value: applyTitleBarInsets,
	});
	syncDocument();
	if (!document.documentElement) {
		document.addEventListener('readystatechange', syncDocument, { once: true });
	}
})();`, platform, overlay)
}
