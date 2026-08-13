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
            "window.webkit.messageHandlers.shellWindowDrag.postMessage(null);"
        "}, true);"
    "})();";
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
    [controller addUserScript:[[WKUserScript alloc]
        initWithSource:ShellWindowDragScript()
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
