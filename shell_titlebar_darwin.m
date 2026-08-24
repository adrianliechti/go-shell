#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>

static char shellWindowControlOriginalFrameKey;
static char shellWindowDragHandlerKey;

@interface ShellWindowDragHandler : NSObject <WKScriptMessageHandler>
@property(weak) NSWindow *window;
@end

@implementation ShellWindowDragHandler
- (void)userContentController:(WKUserContentController *)userContentController
      didReceiveScriptMessage:(WKScriptMessage *)message {
    NSWindow *window = self.window;
    if (window == nil) {
        return;
    }

    if ([message.name isEqualToString:@"shellWindowMaximizeToggle"]) {
        [window zoom:nil];
        return;
    }

    if (![message.name isEqualToString:@"shellWindowDrag"]) {
        return;
    }

    NSEvent *event = NSApp.currentEvent;
    if (event.type != NSEventTypeLeftMouseDown || event.window != window) {
        event = [NSEvent mouseEventWithType:NSEventTypeLeftMouseDown
                                  location:window.mouseLocationOutsideOfEventStream
                             modifierFlags:0
                                 timestamp:NSProcessInfo.processInfo.systemUptime
                              windowNumber:window.windowNumber
                                   context:nil
                               eventNumber:0
                                clickCount:1
                                  pressure:1.0];
    }
    [window performWindowDragWithEvent:event];
}
@end

static NSString *ShellWindowDragScript(void) {
    return @"(() => {"
        "if (window.__shellWindowDragInstalled) return;"
        "window.__shellWindowDragInstalled = true;"
        "document.addEventListener('mousedown', (event) => {"
            "if (event.button !== 0 || !(event.target instanceof Element)) return;"
            "const region = getComputedStyle(event.target)"
                ".getPropertyValue('--shell-window-drag').trim();"
            "if (region !== 'drag') return;"
            "event.preventDefault();"
            "if (event.detail === 2) {"
                "window.webkit.messageHandlers.shellWindowMaximizeToggle.postMessage(null);"
                "return;"
            "}"
            "window.webkit.messageHandlers.shellWindowDrag.postMessage(null);"
        "}, true);"
    "})();";
}

static NSString *ShellTitleBarMetricsScript(void) {
    return @"window.webkit.messageHandlers.shellTitleBar.postMessage(null);";
}

void ShellConfigureTitleBar(NSWindow *window, WKWebView *webView) {
    window.styleMask |= NSWindowStyleMaskFullSizeContentView;
    window.titleVisibility = NSWindowTitleHidden;
    window.titlebarAppearsTransparent = YES;
    window.movableByWindowBackground = YES;
    if (@available(macOS 11.0, *)) {
        window.titlebarSeparatorStyle = NSTitlebarSeparatorStyleNone;
    }

    if (objc_getAssociatedObject(webView, &shellWindowDragHandlerKey) != nil) {
        return;
    }

    ShellWindowDragHandler *handler = [ShellWindowDragHandler new];
    handler.window = window;
    objc_setAssociatedObject(
        webView,
        &shellWindowDragHandlerKey,
        handler,
        OBJC_ASSOCIATION_RETAIN_NONATOMIC
    );

    WKUserContentController *controller = webView.configuration.userContentController;
    [controller addScriptMessageHandler:handler name:@"shellWindowDrag"];
    [controller addScriptMessageHandler:handler name:@"shellWindowMaximizeToggle"];
    [controller addUserScript:[[WKUserScript alloc]
        initWithSource:ShellWindowDragScript()
        injectionTime:WKUserScriptInjectionTimeAtDocumentStart
        forMainFrameOnly:YES]];
    [controller addUserScript:[[WKUserScript alloc]
        initWithSource:ShellTitleBarMetricsScript()
        injectionTime:WKUserScriptInjectionTimeAtDocumentStart
        forMainFrameOnly:YES]];
}

void ShellPositionTitleBarControls(NSWindow *window, NSInteger offsetX, NSInteger offsetY) {
    NSArray<NSNumber *> *buttonTypes = @[
        @(NSWindowCloseButton),
        @(NSWindowMiniaturizeButton),
        @(NSWindowZoomButton),
    ];
    for (NSNumber *value in buttonTypes) {
        NSButton *button = [window standardWindowButton:value.unsignedIntegerValue];
        if (button == nil || button.superview == nil) {
            continue;
        }
        NSValue *storedFrame = objc_getAssociatedObject(
            button,
            &shellWindowControlOriginalFrameKey
        );
        if (storedFrame == nil) {
            storedFrame = [NSValue valueWithRect:button.frame];
            objc_setAssociatedObject(
                button,
                &shellWindowControlOriginalFrameKey,
                storedFrame,
                OBJC_ASSOCIATION_RETAIN_NONATOMIC
            );
        }
        NSRect frame = storedFrame.rectValue;
        frame.origin.x += offsetX;
        frame.origin.y += button.superview.isFlipped ? offsetY : -offsetY;
        button.frame = frame;
    }
}

void ShellPublishTitleBarMetrics(NSWindow *window, WKWebView *webView, NSInteger requestedHeight) {
    if (window == nil || webView == nil) {
        return;
    }

    NSArray<NSNumber *> *buttonTypes = @[
        @(NSWindowCloseButton),
        @(NSWindowMiniaturizeButton),
        @(NSWindowZoomButton),
    ];

    CGFloat insetLeft = 0;
    CGFloat controlsHeight = 0;

    for (NSNumber *value in buttonTypes) {
        NSButton *button = [window standardWindowButton:value.unsignedIntegerValue];
        if (button == nil || button.superview == nil) {
            continue;
        }

        NSRect frame = [webView convertRect:button.bounds fromView:button];
        insetLeft = MAX(insetLeft, NSMaxX(frame));

        CGFloat bottom = webView.isFlipped
            ? NSMaxY(frame)
            : NSHeight(webView.bounds) - NSMinY(frame);
        controlsHeight = MAX(controlsHeight, bottom);
    }

    // Keep application controls from touching the traffic lights even when the
    // caller nudges them inward with ControlsOffsetX.
    NSInteger left = insetLeft > 0 ? (NSInteger)ceil(insetLeft + 8.0) : 0;
    NSInteger height = requestedHeight;

    if (height <= 0) {
        CGFloat layoutHeight = NSHeight(webView.bounds) - NSHeight(window.contentLayoutRect);
        height = (NSInteger)ceil(MAX(layoutHeight, controlsHeight + 8.0));
    }

    BOOL maximized = window.isZoomed || (window.styleMask & NSWindowStyleMaskFullScreen) != 0;
    NSString *script = [NSString stringWithFormat:
        @"window.__shellApplyTitleBarInsets?.({height:%ld,left:%ld,right:0,maximized:%@});",
        (long)height,
        (long)left,
        maximized ? @"true" : @"false"];
    [webView evaluateJavaScript:script completionHandler:nil];
}
