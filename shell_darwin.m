#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

#include <stdint.h>
#include <string.h>

extern void shellFolderPicked(char *path, uintptr_t ctx);

@interface ShellApp : NSObject <NSApplicationDelegate, WKUIDelegate, WKNavigationDelegate, WKDownloadDelegate>

@property(strong) NSWindow *window;
@property(strong) WKWebView *webView;
@property(strong) NSURL *baseURL;
@property(strong) NSMapTable<WKDownload *, NSURL *> *downloads;

@end

@implementation ShellApp

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender {
    return YES;
}

// Intercept quit (Cmd+Q, dock, last window closed) and stop the run loop
// instead of terminating the process, so ShellRun — and with it the Go
// shell.Run — returns and the app's deferred cleanup gets to run.
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
    [NSApp stop:nil];

    // stop: only takes effect once an event is processed — nudge the loop.
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

    return NSTerminateCancel;
}

- (BOOL)applicationSupportsSecureRestorableState:(NSApplication *)app {
    return YES;
}

- (BOOL)isAppURL:(NSURL *)url {
    return [url.scheme isEqualToString:self.baseURL.scheme] &&
           [url.host isEqualToString:self.baseURL.host] &&
           ((url.port == nil && self.baseURL.port == nil) || [url.port isEqualToNumber:self.baseURL.port]);
}

#pragma mark - Navigation

// target="_blank" and window.open land here: open externals in the default
// browser, keep same-origin targets in the app window (never spawn a window).
- (WKWebView *)webView:(WKWebView *)webView
    createWebViewWithConfiguration:(WKWebViewConfiguration *)configuration
               forNavigationAction:(WKNavigationAction *)navigationAction
                    windowFeatures:(WKWindowFeatures *)windowFeatures {
    NSURL *url = navigationAction.request.URL;

    if (url == nil) {
        return nil;
    }

    if ([self isAppURL:url]) {
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
    if (url != nil && isMainFrame && ![self isAppURL:url]) {
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

- (void)webView:(WKWebView *)webView navigationAction:(WKNavigationAction *)navigationAction didBecomeDownload:(WKDownload *)download {
    download.delegate = self;
}

- (void)webView:(WKWebView *)webView navigationResponse:(WKNavigationResponse *)navigationResponse didBecomeDownload:(WKDownload *)download {
    download.delegate = self;
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

#pragma mark - Zoom

- (void)zoomIn:(id)sender {
    self.webView.pageZoom = MIN(self.webView.pageZoom * 1.1, 3.0);
}

- (void)zoomOut:(id)sender {
    self.webView.pageZoom = MAX(self.webView.pageZoom / 1.1, 0.5);
}

- (void)actualSize:(id)sender {
    self.webView.pageZoom = 1.0;
}

@end

static NSMenuItem *MenuItem(NSString *title, SEL action, NSString *key, NSEventModifierFlags modifiers) {
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:action keyEquivalent:key];

    if (modifiers != 0) {
        item.keyEquivalentModifierMask = modifiers;
    }

    return item;
}

static NSMenuItem *TargetedMenuItem(NSString *title, id target, SEL action, NSString *key) {
    NSMenuItem *item = MenuItem(title, action, key, 0);
    item.target = target;
    return item;
}

// A minimal but complete menu bar: without an Edit menu, Cmd+C/V/X and
// friends do not reach the WKWebView at all.
static void BuildMenu(ShellApp *delegate) {
    NSString *appName = [NSProcessInfo processInfo].processName;

    NSMenu *appMenu = [[NSMenu alloc] initWithTitle:appName];
    [appMenu addItem:MenuItem([@"Hide " stringByAppendingString:appName], @selector(hide:), @"h", 0)];
    [appMenu addItem:MenuItem(@"Hide Others", @selector(hideOtherApplications:), @"h", NSEventModifierFlagCommand | NSEventModifierFlagOption)];
    [appMenu addItem:MenuItem(@"Show All", @selector(unhideAllApplications:), @"", 0)];
    [appMenu addItem:[NSMenuItem separatorItem]];
    [appMenu addItem:MenuItem([@"Quit " stringByAppendingString:appName], @selector(terminate:), @"q", 0)];

    NSMenu *fileMenu = [[NSMenu alloc] initWithTitle:@"File"];
    [fileMenu addItem:MenuItem(@"Close Window", @selector(performClose:), @"w", 0)];

    NSMenu *editMenu = [[NSMenu alloc] initWithTitle:@"Edit"];
    [editMenu addItem:MenuItem(@"Undo", @selector(undo:), @"z", 0)];
    [editMenu addItem:MenuItem(@"Redo", @selector(redo:), @"Z", 0)];
    [editMenu addItem:[NSMenuItem separatorItem]];
    [editMenu addItem:MenuItem(@"Cut", @selector(cut:), @"x", 0)];
    [editMenu addItem:MenuItem(@"Copy", @selector(copy:), @"c", 0)];
    [editMenu addItem:MenuItem(@"Paste", @selector(paste:), @"v", 0)];
    [editMenu addItem:MenuItem(@"Select All", @selector(selectAll:), @"a", 0)];

    NSMenu *viewMenu = [[NSMenu alloc] initWithTitle:@"View"];
    NSMenuItem *reload = MenuItem(@"Reload", @selector(reload:), @"r", 0);
    reload.target = delegate.webView;
    [viewMenu addItem:reload];
    [viewMenu addItem:[NSMenuItem separatorItem]];
    [viewMenu addItem:TargetedMenuItem(@"Actual Size", delegate, @selector(actualSize:), @"0")];
    [viewMenu addItem:TargetedMenuItem(@"Zoom In", delegate, @selector(zoomIn:), @"+")];
    [viewMenu addItem:TargetedMenuItem(@"Zoom Out", delegate, @selector(zoomOut:), @"-")];
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

        NSWindow *window = nil;

        if ([NSApp.delegate isKindOfClass:[ShellApp class]]) {
            window = ((ShellApp *)NSApp.delegate).window;
        }

        if (window != nil) {
            [panel beginSheetModalForWindow:window completionHandler:finish];
        } else {
            finish([panel runModal]);
        }
    });
}

void ShellRun(const char *url, const char *title, int width, int height, int minWidth, int minHeight, int debug) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];

        NSString *titleString = [NSString stringWithUTF8String:title];

        NSRect frame = NSMakeRect(0, 0, width, height);
        NSWindowStyleMask style = NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
                                  NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable;

        NSWindow *window = [[NSWindow alloc] initWithContentRect:frame
                                                       styleMask:style
                                                         backing:NSBackingStoreBuffered
                                                           defer:NO];
        window.title = titleString;
        window.tabbingMode = NSWindowTabbingModeDisallowed;

        // Matches the system appearance, so the moment before the first page
        // paint doesn't flash white in dark mode.
        window.backgroundColor = [NSColor windowBackgroundColor];

        if (minWidth > 0 || minHeight > 0) {
            window.minSize = NSMakeSize(minWidth, minHeight);
        }

        [window center];
        [window setFrameAutosaveName:[titleString stringByAppendingString:@"Window"]];

        WKWebViewConfiguration *configuration = [WKWebViewConfiguration new];

        if (debug) {
            [configuration.preferences setValue:@YES forKey:@"developerExtrasEnabled"];
        }

        WKWebView *webView = [[WKWebView alloc] initWithFrame:frame configuration:configuration];

        // Draw the window background instead of white until the page paints.
        @try {
            [webView setValue:@NO forKey:@"drawsBackground"];
        } @catch (NSException *exception) {
        }

        if (@available(macOS 13.3, *)) {
            webView.inspectable = debug ? YES : NO;
        }

        ShellApp *delegate = [ShellApp new];
        delegate.window = window;
        delegate.webView = webView;
        delegate.baseURL = [NSURL URLWithString:[NSString stringWithUTF8String:url]];
        delegate.downloads = [NSMapTable weakToStrongObjectsMapTable];

        webView.UIDelegate = delegate;
        webView.navigationDelegate = delegate;

        // Keep a permanent strong reference: NSApp.delegate and the WKWebView
        // delegate slots are all weak, so without this the only strong owner
        // is this local, which ARC may release right after its last use here.
        static ShellApp *gDelegate;
        gDelegate = delegate;

        [NSApp setDelegate:delegate];
        BuildMenu(delegate);

        window.contentView = webView;
        [webView loadRequest:[NSURLRequest requestWithURL:delegate.baseURL]];

        [window makeKeyAndOrderFront:nil];
        [NSApp activateIgnoringOtherApps:YES];

        [NSApp run];

        // The run loop was stopped by applicationShouldTerminate: — take the
        // window down before control returns to Go.
        [window close];
    }
}
