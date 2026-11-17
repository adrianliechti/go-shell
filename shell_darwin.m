#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

#include <stdint.h>
#include <string.h>

extern void shellFolderPicked(char *path, uintptr_t ctx);
extern void shellShutdown(uintptr_t ctx);
extern void shellDrainWindowRequests(int stopping);
void ShellConfigureTitleBar(NSWindow *window, WKWebView *webView);
void ShellPositionTitleBarControls(NSWindow *window, NSInteger offsetX, NSInteger offsetY);
void ShellPublishTitleBarMetrics(NSWindow *window, WKWebView *webView, NSInteger requestedHeight);

static void *ShellTitleContext = &ShellTitleContext;

@class ShellWindow;

// ShellApp is the application delegate. It holds what all windows share — the
// options, the menu and the downloads — and quits the app.
@interface ShellApp : NSObject <NSApplicationDelegate, WKDownloadDelegate>

@property(strong) NSURL *baseURL;
@property(copy) NSString *title;
@property NSInteger width;
@property NSInteger height;
@property NSInteger minWidth;
@property NSInteger minHeight;
@property BOOL debug;
@property BOOL microphone;
@property BOOL titleBarOverlay;
@property NSInteger titleBarHeight;
@property NSInteger controlsOffsetX;
@property NSInteger controlsOffsetY;
@property(copy) NSString *shellEnvironment;
@property(strong) NSMutableArray<ShellWindow *> *windows;
@property(weak) ShellWindow *lastKeyWindow;
@property(strong) NSMutableDictionary<NSString *, NSMenuItem *> *commandItems;
@property(strong) NSMutableDictionary<NSString *, NSNumber *> *commandDefaults;
@property(strong) NSMapTable<WKDownload *, NSURL *> *downloads;
@property uintptr_t shutdownContext;
@property BOOL shutdownStarted;

- (ShellWindow *)openWindowWithURL:(NSURL *)url;
- (ShellWindow *)activeWindow;
- (BOOL)isAppURL:(NSURL *)url;
- (void)showCommandStatesOf:(ShellWindow *)window;

@end

// ShellWindow is one window and its web view: it answers the page's dialogs
// and navigations, and remembers which File menu commands its page enabled.
@interface ShellWindow : NSObject <NSWindowDelegate, WKUIDelegate, WKNavigationDelegate, WKScriptMessageHandler>

@property(weak) ShellApp *app;
@property(strong) NSWindow *window;
@property(strong) WKWebView *webView;
@property(strong) NSMutableDictionary<NSString *, NSNumber *> *commandStates;
@property BOOL closed;

@end

// The application delegate lives for the whole run: NSApp.delegate is weak.
static ShellApp *gApp;

@implementation ShellWindow

- (void)userContentController:(WKUserContentController *)userContentController
      didReceiveScriptMessage:(WKScriptMessage *)message {
    if ([message.name isEqualToString:@"shellTitleBar"]) {
        [self publishTitleBarMetrics];
        return;
    }

    if (![message.name isEqualToString:@"shellCommandState"] ||
        ![message.body isKindOfClass:[NSDictionary class]]) {
        return;
    }

    NSDictionary *state = message.body;
    NSString *command = state[@"command"];
    NSNumber *enabled = state[@"enabled"];
    if (![command isKindOfClass:[NSString class]] ||
        ![enabled isKindOfClass:[NSNumber class]]) {
        return;
    }

    // The menu shows the commands of the window in front.
    self.commandStates[command] = @(enabled.boolValue);
    if (self.app.activeWindow == self) {
        [self.app showCommandStatesOf:self];
    }
}

- (void)publishTitleBarMetrics {
    if (!self.app.titleBarOverlay) {
        return;
    }

    ShellPublishTitleBarMetrics(self.window, self.webView, self.app.titleBarHeight);
}

- (void)positionWindowControls {
    if (!self.app.titleBarOverlay) {
        return;
    }
    ShellPositionTitleBarControls(
        self.window,
        self.app.controlsOffsetX,
        self.app.controlsOffsetY
    );
    [self publishTitleBarMetrics];
}

#pragma mark - Menu actions

// AppKit routes menu actions through the focused window's responder chain.

- (void)shellPerformCommand:(NSMenuItem *)sender {
    NSString *command = sender.representedObject;
    if (command.length == 0) {
        return;
    }

    NSData *encoded = [NSJSONSerialization dataWithJSONObject:command
                                                      options:NSJSONWritingFragmentsAllowed
                                                        error:nil];
    if (encoded == nil) {
        return;
    }

    NSString *detail = [[NSString alloc] initWithData:encoded encoding:NSUTF8StringEncoding];
    NSString *script = [NSString stringWithFormat:
        @"window.dispatchEvent(new CustomEvent('shell:command',{detail:%@}));",
        detail];
    [self.webView evaluateJavaScript:script completionHandler:nil];
}

- (void)shellReload:(id)sender {
    [self.webView reload];
}

- (void)shellZoomIn:(id)sender {
    self.webView.pageZoom = MIN(self.webView.pageZoom * 1.1, 3.0);
}

- (void)shellZoomOut:(id)sender {
    self.webView.pageZoom = MAX(self.webView.pageZoom / 1.1, 0.5);
}

- (void)shellActualSize:(id)sender {
    self.webView.pageZoom = 1.0;
}

#pragma mark - Window

- (void)windowDidResize:(NSNotification *)notification {
    [self positionWindowControls];
    __weak ShellWindow *window = self;
    dispatch_async(dispatch_get_main_queue(), ^{
        [window positionWindowControls];
    });
}

- (void)windowDidBecomeKey:(NSNotification *)notification {
    self.app.lastKeyWindow = self;
    [self.app showCommandStatesOf:self];
}

- (void)windowWillClose:(NSNotification *)notification {
    if (self.closed) {
        return;
    }
    self.closed = YES;

    [self.webView removeObserver:self forKeyPath:@"title" context:ShellTitleContext];
    // The content controller holds its message handlers strongly, this
    // window among them.
    if (@available(macOS 11.0, *)) {
        [self.webView.configuration.userContentController removeAllScriptMessageHandlers];
    }

    if (!self.app.shutdownStarted) {
        [self.app showCommandStatesOf:self.app.activeWindow];
        // Keep the window in the registry until AppKit finishes closing it.
        // ShellRun also releases the registry if the event loop exits first.
        __weak ShellWindow *closing = self;
        dispatch_async(dispatch_get_main_queue(), ^{
            ShellWindow *window = closing;
            [window.app.windows removeObjectIdenticalTo:window];
        });
    }
}

// The window takes its page's title, so the Window menu tells windows apart.
- (void)observeValueForKeyPath:(NSString *)keyPath
                      ofObject:(id)object
                        change:(NSDictionary<NSKeyValueChangeKey, id> *)change
                       context:(void *)context {
    if (context != ShellTitleContext) {
        [super observeValueForKeyPath:keyPath ofObject:object change:change context:context];
        return;
    }

    NSString *title = self.webView.title;
    self.window.title = title.length > 0 ? title : self.app.title;
}

#pragma mark - Navigation

// target="_blank" and window.open land here: open externals in the default
// browser, keep same-origin targets in this window (never spawn a window).
- (WKWebView *)webView:(WKWebView *)webView
    createWebViewWithConfiguration:(WKWebViewConfiguration *)configuration
               forNavigationAction:(WKNavigationAction *)navigationAction
                    windowFeatures:(WKWindowFeatures *)windowFeatures {
    NSURL *url = navigationAction.request.URL;

    if (url == nil) {
        return nil;
    }

    if ([self.app isAppURL:url]) {
        [webView loadRequest:navigationAction.request];
    } else {
        [[NSWorkspace sharedWorkspace] openURL:url];
    }

    return nil;
}

- (void)webView:(WKWebView *)webView
    decidePolicyForNavigationAction:(WKNavigationAction *)navigationAction
                    decisionHandler:(void (^)(WKNavigationActionPolicy))decisionHandler {
    // Anchors with a download attribute (including blob: URLs built by the
    // page) become WKDownloads instead of navigations.
    if (navigationAction.shouldPerformDownload) {
        decisionHandler(WKNavigationActionPolicyDownload);
        return;
    }

    NSURL *url = navigationAction.request.URL;
    BOOL isMainFrame = navigationAction.targetFrame != nil && navigationAction.targetFrame.mainFrame;

    // Cover redirects and JS-driven navigations (navigationType Other), not
    // just clicked links: any of them leaving the app's origin would otherwise
    // take over the window with no toolbar or way back.
    if (url != nil && isMainFrame && ![self.app isAppURL:url]) {
        [[NSWorkspace sharedWorkspace] openURL:url];
        decisionHandler(WKNavigationActionPolicyCancel);
        return;
    }

    decisionHandler(WKNavigationActionPolicyAllow);
}

- (void)webView:(WKWebView *)webView
    decidePolicyForNavigationResponse:(WKNavigationResponse *)navigationResponse
                      decisionHandler:(void (^)(WKNavigationResponsePolicy))decisionHandler {
    // Server responses the web view cannot render (archives, binaries, ...)
    // and explicit attachments (Content-Disposition) become downloads instead
    // of dead navigations.
    BOOL attachment = NO;

    if ([navigationResponse.response isKindOfClass:[NSHTTPURLResponse class]]) {
        NSString *disposition = ((NSHTTPURLResponse *)navigationResponse.response).allHeaderFields[@"Content-Disposition"];
        attachment = [disposition.lowercaseString hasPrefix:@"attachment"];
    }

    if (navigationResponse.forMainFrame && (attachment || !navigationResponse.canShowMIMEType)) {
        decisionHandler(WKNavigationResponsePolicyDownload);
        return;
    }

    decisionHandler(WKNavigationResponsePolicyAllow);
}

// Downloads belong to the app, so they finish when their window closes.
- (void)webView:(WKWebView *)webView navigationAction:(WKNavigationAction *)navigationAction didBecomeDownload:(WKDownload *)download {
    download.delegate = self.app;
}

- (void)webView:(WKWebView *)webView navigationResponse:(WKNavigationResponse *)navigationResponse didBecomeDownload:(WKDownload *)download {
    download.delegate = self.app;
}

#pragma mark - JavaScript dialogs

- (void)webView:(WKWebView *)webView
    runJavaScriptAlertPanelWithMessage:(NSString *)message
                      initiatedByFrame:(WKFrameInfo *)frame
                     completionHandler:(void (^)(void))completionHandler {
    NSAlert *alert = [NSAlert new];
    alert.messageText = message ?: @"";
    [alert addButtonWithTitle:@"OK"];

    [alert beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse response) {
        completionHandler();
    }];
}

- (void)webView:(WKWebView *)webView
    runJavaScriptConfirmPanelWithMessage:(NSString *)message
                        initiatedByFrame:(WKFrameInfo *)frame
                       completionHandler:(void (^)(BOOL))completionHandler {
    NSAlert *alert = [NSAlert new];
    alert.messageText = message ?: @"";
    [alert addButtonWithTitle:@"OK"];
    [alert addButtonWithTitle:@"Cancel"];

    [alert beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse response) {
        completionHandler(response == NSAlertFirstButtonReturn);
    }];
}

- (void)webView:(WKWebView *)webView
    runJavaScriptTextInputPanelWithPrompt:(NSString *)prompt
                              defaultText:(NSString *)defaultText
                         initiatedByFrame:(WKFrameInfo *)frame
                        completionHandler:(void (^)(NSString *))completionHandler {
    NSAlert *alert = [NSAlert new];
    alert.messageText = prompt ?: @"";
    [alert addButtonWithTitle:@"OK"];
    [alert addButtonWithTitle:@"Cancel"];

    NSTextField *input = [[NSTextField alloc] initWithFrame:NSMakeRect(0, 0, 260, 24)];
    input.stringValue = defaultText ?: @"";
    alert.accessoryView = input;
    alert.window.initialFirstResponder = input;

    [alert beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse response) {
        completionHandler(response == NSAlertFirstButtonReturn ? input.stringValue : nil);
    }];
}

// getUserMedia: only the app's own top-level page may use the microphone, and
// only when the app opted in; WebKit would otherwise prompt for any request.
- (void)webView:(WKWebView *)webView
    requestMediaCapturePermissionForOrigin:(WKSecurityOrigin *)origin
                          initiatedByFrame:(WKFrameInfo *)frame
                                      type:(WKMediaCaptureType)type
                           decisionHandler:(void (^)(WKPermissionDecision decision))decisionHandler API_AVAILABLE(macos(12.0)) {
    NSURL *base = self.app.baseURL;
    NSInteger port = base.port ? base.port.integerValue : 0;
    BOOL appOrigin = [origin.protocol isEqualToString:base.scheme] &&
                     [origin.host isEqualToString:base.host] &&
                     origin.port == port;
    BOOL allowed = self.app.microphone && type == WKMediaCaptureTypeMicrophone && frame.isMainFrame && appOrigin;
    decisionHandler(allowed ? WKPermissionDecisionGrant : WKPermissionDecisionDeny);
}

// <input type="file">
- (void)webView:(WKWebView *)webView
    runOpenPanelWithParameters:(WKOpenPanelParameters *)parameters
              initiatedByFrame:(WKFrameInfo *)frame
             completionHandler:(void (^)(NSArray<NSURL *> *))completionHandler {
    NSOpenPanel *panel = [NSOpenPanel openPanel];
    panel.canChooseFiles = YES;
    panel.canChooseDirectories = parameters.allowsDirectories;
    panel.allowsMultipleSelection = parameters.allowsMultipleSelection;

    [panel beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse response) {
        completionHandler(response == NSModalResponseOK ? panel.URLs : nil);
    }];
}

@end

@implementation ShellApp

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender {
    return YES;
}

// stop: only takes effect once an event is processed, so nudge the loop.
- (void)stopRunLoop {
    [NSApp stop:nil];
    NSEvent *event = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
                                        location:NSZeroPoint
                                   modifierFlags:0
                                       timestamp:0
                                    windowNumber:0
                                         context:nil
                                         subtype:0
                                           data1:0
                                           data2:0];
    [NSApp postEvent:event atStart:YES];
}

// Intercept quit (Cmd+Q, dock, last window closed) so ShellRun — and with it
// the Go shell.Run — returns instead of AppKit terminating the process. When
// the app supplied a shutdown callback, keep the event loop alive while it
// runs so macOS never sees a frozen app during backend cleanup.
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
    if (self.shutdownStarted) {
        return NSTerminateCancel;
    }

    self.shutdownStarted = YES;
    shellDrainWindowRequests(1);
    for (ShellWindow *window in self.windows) {
        [window.window orderOut:nil];
    }

    if (self.shutdownContext == 0) {
        [self stopRunLoop];
        return NSTerminateCancel;
    }

    uintptr_t shutdownContext = self.shutdownContext;
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{
        shellShutdown(shutdownContext);
        dispatch_async(dispatch_get_main_queue(), ^{
            [NSApp replyToApplicationShouldTerminate:NO];
            [self stopRunLoop];
        });
    });

    return NSTerminateLater;
}

- (BOOL)applicationSupportsSecureRestorableState:(NSApplication *)app {
    return YES;
}

- (BOOL)isAppURL:(NSURL *)url {
    return [url.scheme isEqualToString:self.baseURL.scheme] &&
           [url.host isEqualToString:self.baseURL.host] &&
           ((url.port == nil && self.baseURL.port == nil) || [url.port isEqualToNumber:self.baseURL.port]);
}

- (ShellWindow *)windowFor:(NSWindow *)window {
    if (window == nil) {
        return nil;
    }

    for (ShellWindow *candidate in self.windows) {
        if (!candidate.closed && (candidate.window == window || candidate.window == window.sheetParent)) {
            return candidate;
        }
    }

    return nil;
}

// activeWindow is the window in front, whose commands the menu shows and on
// which the folder picker opens.
- (ShellWindow *)activeWindow {
    ShellWindow *active = [self windowFor:NSApp.keyWindow] ?: [self windowFor:NSApp.mainWindow] ?: self.lastKeyWindow;
    if (active != nil && !active.closed) {
        return active;
    }

    for (ShellWindow *candidate in self.windows.reverseObjectEnumerator) {
        if (!candidate.closed) {
            return candidate;
        }
    }

    return nil;
}

// With every window minimized, the responder chain reaches the app delegate.
- (void)shellPerformCommand:(NSMenuItem *)sender {
    [self.activeWindow shellPerformCommand:sender];
}

- (void)showCommandStatesOf:(ShellWindow *)window {
    [self.commandItems enumerateKeysAndObjectsUsingBlock:^(NSString *command, NSMenuItem *item, BOOL *stop) {
        NSNumber *enabled = window.commandStates[command] ?: self.commandDefaults[command];
        item.enabled = window != nil && enabled.boolValue;
    }];
}

- (ShellWindow *)openWindowWithURL:(NSURL *)url {
    ShellWindow *from = self.activeWindow;

    NSRect frame = NSMakeRect(0, 0, self.width, self.height);
    NSWindowStyleMask style = NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
                              NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable;

    // Request the full-size content view up front: AppKit keeps the content
    // size when the style mask changes later, so adding it after the frame
    // autosave restored the window would shrink it by the title-bar height
    // on every launch, and the deferred title-bar relayout would reset the
    // window controls moved by ControlsOffsetX/Y.
    if (self.titleBarOverlay) {
        style |= NSWindowStyleMaskFullSizeContentView;
    }

    NSWindow *window = [[NSWindow alloc] initWithContentRect:frame
                                                   styleMask:style
                                                     backing:NSBackingStoreBuffered
                                                       defer:NO];
    // ARC owns the window; AppKit must not release it again when it closes.
    window.releasedWhenClosed = NO;
    window.title = self.title;
    window.tabbingMode = NSWindowTabbingModeDisallowed;

    // Matches the system appearance, so the moment before the first page
    // paint doesn't flash white in dark mode.
    window.backgroundColor = [NSColor windowBackgroundColor];

    if (self.minWidth > 0 || self.minHeight > 0) {
        NSSize minimum = window.minSize;
        if (self.minWidth > 0) {
            minimum.width = self.minWidth;
        }
        if (self.minHeight > 0) {
            minimum.height = self.minHeight;
        }
        window.minSize = minimum;
    }

    if (from == nil) {
        [window center];
        [window setFrameAutosaveName:[self.title stringByAppendingString:@"Window"]];
    } else {
        // Further windows take the size of the window in front and open
        // offset from it; only the first one saves its frame.
        NSRect front = from.window.frame;
        [window setFrame:front display:NO];
        NSPoint next = [window cascadeTopLeftFromPoint:NSMakePoint(NSMinX(front), NSMaxY(front))];
        [window cascadeTopLeftFromPoint:next];
    }

    WKWebViewConfiguration *configuration = [WKWebViewConfiguration new];

    if (self.debug) {
        [configuration.preferences setValue:@YES forKey:@"developerExtrasEnabled"];
    }

    WKWebView *webView = [[WKWebView alloc] initWithFrame:frame configuration:configuration];

    // Draw the window background instead of white until the page paints.
    @try {
        [webView setValue:@NO forKey:@"drawsBackground"];
    } @catch (NSException *exception) {
    }

    if (@available(macOS 13.3, *)) {
        webView.inspectable = self.debug;
    }

    ShellWindow *controller = [ShellWindow new];
    controller.app = self;
    controller.window = window;
    controller.webView = webView;
    controller.commandStates = [NSMutableDictionary dictionary];

    window.delegate = controller;
    webView.UIDelegate = controller;
    webView.navigationDelegate = controller;

    NSString *commandStateScript = @"window.addEventListener('shell:command-state',(event)=>{"
        "const state=event.detail;"
        "if(!state||typeof state.command!=='string'||typeof state.enabled!=='boolean')return;"
        "window.webkit.messageHandlers.shellCommandState.postMessage(state);"
    "});";
    WKUserContentController *userContent = webView.configuration.userContentController;
    if (self.shellEnvironment.length > 0) {
        [userContent addUserScript:[[WKUserScript alloc]
            initWithSource:self.shellEnvironment
            injectionTime:WKUserScriptInjectionTimeAtDocumentStart
            forMainFrameOnly:YES]];
    }
    if (self.titleBarOverlay) {
        ShellConfigureTitleBar(window, webView);
    }
    [userContent addScriptMessageHandler:controller name:@"shellCommandState"];
    if (self.titleBarOverlay) {
        [userContent addScriptMessageHandler:controller name:@"shellTitleBar"];
    }
    [userContent addUserScript:[[WKUserScript alloc]
        initWithSource:commandStateScript
        injectionTime:WKUserScriptInjectionTimeAtDocumentStart
        forMainFrameOnly:YES]];

    [webView addObserver:controller forKeyPath:@"title" options:0 context:ShellTitleContext];
    [self.windows addObject:controller];

    window.contentView = webView;
    [webView loadRequest:[NSURLRequest requestWithURL:url]];

    [window makeKeyAndOrderFront:nil];

    if (self.titleBarOverlay) {
        [controller positionWindowControls];
        __weak ShellWindow *opening = controller;
        dispatch_async(dispatch_get_main_queue(), ^{
            [opening positionWindowControls];
        });
    }

    return controller;
}

#pragma mark - Downloads

- (void)download:(WKDownload *)download
    decideDestinationUsingResponse:(NSURLResponse *)response
                 suggestedFilename:(NSString *)suggestedFilename
                 completionHandler:(void (^)(NSURL *))completionHandler {
    NSURL *dir = [NSFileManager.defaultManager URLForDirectory:NSDownloadsDirectory
                                                      inDomain:NSUserDomainMask
                                             appropriateForURL:nil
                                                        create:YES
                                                         error:nil];

    if (dir == nil) {
        completionHandler(nil);
        return;
    }

    NSString *name = suggestedFilename.length > 0 ? suggestedFilename : @"download";
    NSURL *destination = [dir URLByAppendingPathComponent:name];

    NSString *base = name.stringByDeletingPathExtension;
    NSString *extension = name.pathExtension;

    for (int i = 2; [NSFileManager.defaultManager fileExistsAtPath:destination.path]; i++) {
        NSString *candidate = extension.length > 0
            ? [NSString stringWithFormat:@"%@ (%d).%@", base, i, extension]
            : [NSString stringWithFormat:@"%@ (%d)", base, i];
        destination = [dir URLByAppendingPathComponent:candidate];
    }

    [self.downloads setObject:destination forKey:download];
    completionHandler(destination);
}

- (void)downloadDidFinish:(WKDownload *)download {
    NSURL *url = [self.downloads objectForKey:download];

    if (url != nil) {
        // Bounce the Downloads stack in the dock, like browsers do.
        [[NSDistributedNotificationCenter defaultCenter] postNotificationName:@"com.apple.DownloadFileFinished"
                                                                       object:url.path];
    }

    [self.downloads removeObjectForKey:download];
}

- (void)download:(WKDownload *)download didFailWithError:(NSError *)error resumeData:(NSData *)resumeData {
    [self.downloads removeObjectForKey:download];
}

@end

static NSMenuItem *MenuItem(NSString *title, SEL action, NSString *key, NSEventModifierFlags modifiers) {
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:action keyEquivalent:key];

    if (modifiers != 0) {
        item.keyEquivalentModifierMask = modifiers;
    }

    return item;
}

// A minimal but complete menu bar: without an Edit menu, Cmd+C/V/X and
// friends do not reach the WKWebView at all.
static NSMenuItem *CommandMenuItem(ShellApp *app, NSDictionary *definition) {
    NSString *title = definition[@"title"];
    NSString *command = definition[@"command"];
    NSString *key = definition[@"key"] ?: @"";
    if (title.length == 0 || command.length == 0) {
        return nil;
    }

    NSMenuItem *item = MenuItem(title, @selector(shellPerformCommand:), key, 0);
    item.representedObject = command;
    item.enabled = ![definition[@"disabled"] boolValue];
    app.commandItems[command] = item;
    app.commandDefaults[command] = @(item.enabled);

    NSEventModifierFlags modifiers = NSEventModifierFlagCommand;
    if ([definition[@"shift"] boolValue]) {
        modifiers |= NSEventModifierFlagShift;
    }
    if ([definition[@"alt"] boolValue]) {
        modifiers |= NSEventModifierFlagOption;
    }
    if ([definition[@"control"] boolValue]) {
        modifiers |= NSEventModifierFlagControl;
    }
    item.keyEquivalentModifierMask = modifiers;
    return item;
}

static void BuildMenu(ShellApp *app, NSArray *customFileItems) {
    NSString *appName = [NSProcessInfo processInfo].processName;

    NSMenu *appMenu = [[NSMenu alloc] initWithTitle:appName];
    [appMenu addItem:MenuItem([@"Hide " stringByAppendingString:appName], @selector(hide:), @"h", 0)];
    [appMenu addItem:MenuItem(@"Hide Others", @selector(hideOtherApplications:), @"h", NSEventModifierFlagCommand | NSEventModifierFlagOption)];
    [appMenu addItem:MenuItem(@"Show All", @selector(unhideAllApplications:), @"", 0)];
    [appMenu addItem:[NSMenuItem separatorItem]];
    [appMenu addItem:MenuItem([@"Quit " stringByAppendingString:appName], @selector(terminate:), @"q", 0)];

    NSMenu *fileMenu = [[NSMenu alloc] initWithTitle:@"File"];
    // Custom commands manage their own enabled state. AppKit's default menu
    // validation would otherwise re-enable every item whose target implements
    // shellPerformCommand:, overriding both Disabled and browser state updates.
    fileMenu.autoenablesItems = NO;
    for (id value in customFileItems) {
        if (![value isKindOfClass:[NSDictionary class]]) {
            continue;
        }
        NSDictionary *definition = value;
        if ([definition[@"separator"] boolValue]) {
            [fileMenu addItem:[NSMenuItem separatorItem]];
            continue;
        }
        NSMenuItem *item = CommandMenuItem(app, definition);
        if (item != nil) {
            [fileMenu addItem:item];
        }
    }
    [fileMenu addItem:MenuItem(@"Close Window", @selector(performClose:), @"W", 0)];

    NSMenu *editMenu = [[NSMenu alloc] initWithTitle:@"Edit"];
    [editMenu addItem:MenuItem(@"Undo", @selector(undo:), @"z", 0)];
    [editMenu addItem:MenuItem(@"Redo", @selector(redo:), @"Z", 0)];
    [editMenu addItem:[NSMenuItem separatorItem]];
    [editMenu addItem:MenuItem(@"Cut", @selector(cut:), @"x", 0)];
    [editMenu addItem:MenuItem(@"Copy", @selector(copy:), @"c", 0)];
    [editMenu addItem:MenuItem(@"Paste", @selector(paste:), @"v", 0)];
    [editMenu addItem:MenuItem(@"Select All", @selector(selectAll:), @"a", 0)];

    NSMenu *viewMenu = [[NSMenu alloc] initWithTitle:@"View"];
    [viewMenu addItem:MenuItem(@"Reload", @selector(shellReload:), @"r", 0)];
    [viewMenu addItem:[NSMenuItem separatorItem]];
    [viewMenu addItem:MenuItem(@"Actual Size", @selector(shellActualSize:), @"0", 0)];
    [viewMenu addItem:MenuItem(@"Zoom In", @selector(shellZoomIn:), @"+", 0)];
    [viewMenu addItem:MenuItem(@"Zoom Out", @selector(shellZoomOut:), @"-", 0)];
    [viewMenu addItem:[NSMenuItem separatorItem]];
    [viewMenu addItem:MenuItem(@"Enter Full Screen", @selector(toggleFullScreen:), @"f", NSEventModifierFlagCommand | NSEventModifierFlagControl)];

    NSMenu *windowMenu = [[NSMenu alloc] initWithTitle:@"Window"];
    [windowMenu addItem:MenuItem(@"Minimize", @selector(performMiniaturize:), @"m", 0)];
    [windowMenu addItem:MenuItem(@"Zoom", @selector(performZoom:), @"", 0)];

    NSMenu *menubar = [NSMenu new];

    for (NSMenu *menu in @[ appMenu, fileMenu, editMenu, viewMenu, windowMenu ]) {
        NSMenuItem *item = [NSMenuItem new];
        item.submenu = menu;
        [menubar addItem:item];
    }

    NSApp.mainMenu = menubar;
    NSApp.windowsMenu = windowMenu;
}

void ShellPickFolder(const char *title, uintptr_t ctx) {
    NSString *message = title ? [NSString stringWithUTF8String:title] : nil;

    dispatch_async(dispatch_get_main_queue(), ^{
        NSOpenPanel *panel = [NSOpenPanel openPanel];
        panel.canChooseFiles = NO;
        panel.canChooseDirectories = YES;
        panel.canCreateDirectories = YES;
        panel.allowsMultipleSelection = NO;

        if (message.length > 0) {
            panel.message = message;
        }

        void (^finish)(NSModalResponse) = ^(NSModalResponse response) {
            NSURL *url = response == NSModalResponseOK ? panel.URLs.firstObject : nil;
            shellFolderPicked(url ? strdup(url.path.fileSystemRepresentation) : NULL, ctx);
        };

        NSWindow *window = gApp.activeWindow.window;

        if (window != nil) {
            [panel beginSheetModalForWindow:window completionHandler:finish];
        } else {
            finish([panel runModal]);
        }
    });
}

void ShellScheduleNewWindows(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        shellDrainWindowRequests(0);
    });
}

// Called on the main thread while draining window requests.
int ShellOpenWindow(const char *url) {
    NSString *address = [NSString stringWithUTF8String:url];
    NSURL *target = address ? [NSURL URLWithString:address] : nil;

    if (gApp.shutdownStarted || target == nil || ![gApp isAppURL:target]) {
        return 0;
    }
    return [gApp openWindowWithURL:target] != nil;
}

void ShellRun(
    const char *url,
    const char *title,
    int width,
    int height,
    int minWidth,
    int minHeight,
    int debug,
    int microphone,
    int titleBarOverlay,
    int titleBarHeight,
    int controlsOffsetX,
    int controlsOffsetY,
    const char *shellEnvironment,
    const char *fileMenu,
    uintptr_t shutdownContext
) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];

        ShellApp *app = [ShellApp new];
        app.baseURL = [NSURL URLWithString:[NSString stringWithUTF8String:url]];
        app.title = [NSString stringWithUTF8String:title];
        app.width = width;
        app.height = height;
        app.minWidth = minWidth;
        app.minHeight = minHeight;
        app.debug = debug ? YES : NO;
        app.microphone = microphone ? YES : NO;
        app.titleBarOverlay = titleBarOverlay ? YES : NO;
        app.titleBarHeight = titleBarHeight;
        app.controlsOffsetX = controlsOffsetX;
        app.controlsOffsetY = controlsOffsetY;
        app.shellEnvironment = shellEnvironment != NULL ? [NSString stringWithUTF8String:shellEnvironment] : nil;
        app.windows = [NSMutableArray array];
        app.commandItems = [NSMutableDictionary dictionary];
        app.commandDefaults = [NSMutableDictionary dictionary];
        app.downloads = [NSMapTable weakToStrongObjectsMapTable];
        app.shutdownContext = shutdownContext;

        gApp = app;
        [NSApp setDelegate:app];

        NSArray *customFileItems = @[];
        if (fileMenu != NULL && strlen(fileMenu) > 0) {
            NSData *data = [[NSString stringWithUTF8String:fileMenu]
                dataUsingEncoding:NSUTF8StringEncoding];
            id parsed = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
            if ([parsed isKindOfClass:[NSArray class]]) {
                customFileItems = parsed;
            }
        }
        BuildMenu(app, customFileItems);

        [app openWindowWithURL:app.baseURL];
        [NSApp activateIgnoringOtherApps:YES];

        [NSApp run];

        // The run loop was stopped by applicationShouldTerminate: — take the
        // windows down before control returns to Go.
        for (ShellWindow *window in [app.windows copy]) {
            if (!window.closed) {
                [window.window close];
            }
        }
        [app.windows removeAllObjects];
        NSApp.delegate = nil;
        gApp = nil;
    }
}
