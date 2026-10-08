#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#import <LocalAuthentication/LocalAuthentication.h>
#import <PDFKit/PDFKit.h>
#import "_cgo_export.h"
#import "bridge.h"
#import "pdfengine.h"

@interface SoCPrintDelegate : NSObject <NSApplicationDelegate, WKScriptMessageHandler, WKNavigationDelegate, WKUIDelegate, WKURLSchemeHandler>
@property(nonatomic, strong) NSWindow *window;
@property(nonatomic, strong) WKWebView *webView;
@property(nonatomic, strong) NSDictionary *resources;
@property(nonatomic, strong) SPPDFController *pdf;
@property(nonatomic) dispatch_queue_t pdfQueue;
@property(nonatomic) NSUInteger accountRevision;
@property(nonatomic, strong) NSMutableSet *approvedKeys;
@end

static SoCPrintDelegate *retainedDelegate = nil;
static __weak WKWebView *activeWebView = nil;

static NSString *jsonString(id value) {
    if (![NSJSONSerialization isValidJSONObject:value]) {
        value = @{ @"Error": @"The app could not prepare the response." };
    }
    NSData *data = [NSJSONSerialization dataWithJSONObject:value options:NSJSONWritingFragmentsAllowed error:nil];
    return [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding] ?: @"{}";
}

static void deliverNativeResponse(NSString *requestID, BOOL ok, NSDictionary *data, NSString *code, NSString *message) {
    NSMutableDictionary *response = [@{
        @"ID": requestID ?: @"",
        @"OK": @(ok),
        @"Data": data ?: @{}
    } mutableCopy];
    if (code) response[@"Code"] = code;
    if (message) response[@"Error"] = message;
    socprint_respond(jsonString(response).UTF8String);
}

@implementation SoCPrintDelegate

- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
    NSRect frame = NSMakeRect(0, 0, 1100, 780);
    NSWindowStyleMask style = NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
        NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable;
    self.window = [[NSWindow alloc] initWithContentRect:frame styleMask:style
        backing:NSBackingStoreBuffered defer:NO];
    self.window.title = @"SimplyPrint @ SoC";
    self.window.minSize = NSMakeSize(820, 600);
    [self.window center];

    WKWebViewConfiguration *configuration = [[WKWebViewConfiguration alloc] init];
    WKUserContentController *controller = [[WKUserContentController alloc] init];
    [controller addScriptMessageHandler:self name:@"socprint"];
    configuration.userContentController = controller;
    configuration.websiteDataStore = WKWebsiteDataStore.nonPersistentDataStore;
    [configuration setURLSchemeHandler:self forURLScheme:@"socprint"];
    self.pdf = [[SPPDFController alloc] init];
    self.pdfQueue = dispatch_queue_create("app.printatsoc.pdf", DISPATCH_QUEUE_SERIAL);
    self.approvedKeys = [NSMutableSet set];
    self.webView = [[WKWebView alloc] initWithFrame:self.window.contentView.bounds configuration:configuration];
    self.webView.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    self.webView.navigationDelegate = self;
    // WebKit does not display JavaScript alert/confirm dialogs unless a UI
    // delegate handles them. The print flow uses confirm() as its final
    // safeguard, so leaving this unset made Print appear to do nothing.
    self.webView.UIDelegate = self;
    activeWebView = self.webView;
    self.window.contentView = self.webView;
    [self.webView loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:@"socprint://app/index.html"]]];

    [self.window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
    [self installMenu];
}

- (void)installMenu {
    NSMenu *menu=[[NSMenu alloc] initWithTitle:@""];
    NSMenuItem *appItem=[[NSMenuItem alloc] initWithTitle:@"" action:nil keyEquivalent:@""];
    NSMenu *appMenu=[[NSMenu alloc] initWithTitle:@"SimplyPrint @ SoC"];
    NSMenuItem *about=[appMenu addItemWithTitle:@"About SimplyPrint @ SoC" action:@selector(showAbout:) keyEquivalent:@""];about.target=self;
    [appMenu addItem:NSMenuItem.separatorItem];
    NSMenuItem *settings=[appMenu addItemWithTitle:@"Account Settings…" action:@selector(showSettings:) keyEquivalent:@","];settings.target=self;
    [appMenu addItem:NSMenuItem.separatorItem];
    [appMenu addItemWithTitle:@"Quit SimplyPrint @ SoC" action:@selector(terminate:) keyEquivalent:@"q"];
    appItem.submenu=appMenu;[menu addItem:appItem];
    NSMenuItem *fileItem=[[NSMenuItem alloc] initWithTitle:@"File" action:nil keyEquivalent:@""];
    NSMenu *file=[[NSMenu alloc] initWithTitle:@"File"];
    NSMenuItem *open=[file addItemWithTitle:@"Open PDF…" action:@selector(openPDF:) keyEquivalent:@"o"];open.target=self;
    NSMenuItem *print=[file addItemWithTitle:@"Print…" action:@selector(printDocument:) keyEquivalent:@"p"];print.target=self;
    fileItem.submenu=file;[menu addItem:fileItem];
    NSMenuItem *editItem=[[NSMenuItem alloc] initWithTitle:@"Edit" action:nil keyEquivalent:@""];
    NSMenu *edit=[[NSMenu alloc] initWithTitle:@"Edit"];
    [edit addItemWithTitle:@"Undo" action:@selector(undo:) keyEquivalent:@"z"];
    NSMenuItem *redo=[edit addItemWithTitle:@"Redo" action:@selector(redo:) keyEquivalent:@"z"];redo.keyEquivalentModifierMask=NSEventModifierFlagCommand|NSEventModifierFlagShift;
    [edit addItem:NSMenuItem.separatorItem];
    [edit addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];
    [edit addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
    [edit addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
    [edit addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
    editItem.submenu=edit;[menu addItem:editItem];NSApp.mainMenu=menu;
}
- (void)sendCommand:(NSString *)command {
    if(SPTrustedURL(self.webView.URL))[self.webView evaluateJavaScript:[NSString stringWithFormat:@"window.socprintCommand(%@)",jsonString(@[command])] completionHandler:nil];
}
- (void)openPDF:(id)sender { [self sendCommand:@"open"]; }
- (void)printDocument:(id)sender { [self sendCommand:@"print"]; }
- (void)showSettings:(id)sender { [self sendCommand:@"settings"]; }
- (void)showAbout:(id)sender {
    [NSApp orderFrontStandardAboutPanelWithOptions:@{NSAboutPanelOptionApplicationName:@"SimplyPrint @ SoC",NSAboutPanelOptionApplicationVersion:[[NSBundle mainBundle] objectForInfoDictionaryKey:@"CFBundleShortVersionString"],@"Copyright":@"Independent student project · No telemetry"}];
}
- (void)applicationWillTerminate:(NSNotification *)notification {
    dispatch_sync(self.pdfQueue, ^{ [self.pdf close]; });
}
- (void)webView:(WKWebView *)webView startURLSchemeTask:(id<WKURLSchemeTask>)task {
    NSURL *url=task.request.URL;
    NSString *name=[url.path substringFromIndex:MIN((NSUInteger)1,url.path.length)];
    NSString *content=self.resources[name];
    if(![url.scheme isEqualToString:@"socprint"]||![url.host isEqualToString:@"app"]||!content||url.query||url.user||url.password||url.port) {
        [task didFailWithError:[NSError errorWithDomain:NSURLErrorDomain code:NSURLErrorResourceUnavailable userInfo:nil]];return;
    }
    NSDictionary *types=@{@"html":@"text/html",@"js":@"application/javascript",@"css":@"text/css",@"svg":@"image/svg+xml"};
    NSData *data=[content dataUsingEncoding:NSUTF8StringEncoding];
    [task didReceiveResponse:[[NSURLResponse alloc] initWithURL:url MIMEType:types[name.pathExtension] expectedContentLength:data.length textEncodingName:@"utf-8"]];
    [task didReceiveData:data];[task didFinish];
}
- (void)webView:(WKWebView *)webView stopURLSchemeTask:(id<WKURLSchemeTask>)task {}
- (WKWebView *)webView:(WKWebView *)webView createWebViewWithConfiguration:(WKWebViewConfiguration *)configuration forNavigationAction:(WKNavigationAction *)action windowFeatures:(WKWindowFeatures *)features { return nil; }

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender {
    return YES;
}

- (void)webView:(WKWebView *)webView runJavaScriptAlertPanelWithMessage:(NSString *)message
    initiatedByFrame:(WKFrameInfo *)frame completionHandler:(void (^)(void))completionHandler {
    NSAlert *alert = [[NSAlert alloc] init];
    alert.messageText = @"SimplyPrint @ SoC";
    alert.informativeText = message ?: @"";
    alert.alertStyle = NSAlertStyleInformational;
    [alert addButtonWithTitle:@"OK"];
    [alert beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse response) {
        completionHandler();
    }];
}

- (void)webView:(WKWebView *)webView runJavaScriptConfirmPanelWithMessage:(NSString *)message
    initiatedByFrame:(WKFrameInfo *)frame completionHandler:(void (^)(BOOL result))completionHandler {
    if(!SPTrustedMessage(webView,frame,self.webView)){completionHandler(NO);return;}
    BOOL printing=[message hasPrefix:@"Print “"];
    NSAlert *alert = [[NSAlert alloc] init];
    alert.messageText = printing ? @"Print document?" : @"Confirm";
    alert.informativeText = message ?: @"";
    alert.alertStyle = NSAlertStyleInformational;
    [alert addButtonWithTitle:@"Cancel"];
    [alert addButtonWithTitle:printing ? @"Print" : @"Continue"];
    [alert beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse response) {
        completionHandler(response == NSAlertSecondButtonReturn);
    }];
}

- (void)webView:(WKWebView *)webView runJavaScriptTextInputPanelWithPrompt:(NSString *)prompt
    defaultText:(NSString *)defaultText initiatedByFrame:(WKFrameInfo *)frame
    completionHandler:(void (^)(NSString *result))completionHandler {
    NSAlert *alert = [[NSAlert alloc] init];
    alert.messageText = @"SimplyPrint @ SoC";
    alert.informativeText = prompt ?: @"";
    alert.alertStyle = NSAlertStyleInformational;
    NSTextField *input = [[NSTextField alloc] initWithFrame:NSMakeRect(0, 0, 300, 24)];
    input.stringValue = defaultText ?: @"";
    alert.accessoryView = input;
    [alert addButtonWithTitle:@"Cancel"];
    [alert addButtonWithTitle:@"Continue"];
    [alert beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse response) {
        completionHandler(response == NSAlertSecondButtonReturn ? input.stringValue : nil);
    }];
}

- (void)userContentController:(WKUserContentController *)userContentController didReceiveScriptMessage:(WKScriptMessage *)message {
    NSDictionary *body = [message.body isKindOfClass:[NSDictionary class]] ? message.body : nil;
    if (!SPTrustedMessage(message.webView,message.frameInfo,self.webView) || !SPValidRequest(body)) {
        deliverNativeResponse(@"", NO, nil, @"invalidRequest", @"The app could not read that action.");
        return;
    }
    NSString *requestID = [body[@"ID"] isKindOfClass:[NSString class]] ? body[@"ID"] : @"";
    NSString *action = [body[@"Action"] isKindOfClass:[NSString class]] ? body[@"Action"] : @"";
    if (![action isEqualToString:@"initialize"] && ![action isEqualToString:@"acceptTerms"] && ![action isEqualToString:@"quitApp"] && !goTermsAccepted()) {
        deliverNativeResponse(requestID,NO,nil,@"termsRequired",@"Review and accept the terms before continuing.");return;
    }
    if([@[@"saveCredentials",@"signOut",@"createKey",@"useKey"] containsObject:action]) self.accountRevision++;
    if([body[@"KeyPath"] length] && ![self.approvedKeys containsObject:body[@"KeyPath"]]) {
        deliverNativeResponse(requestID,NO,nil,@"invalidKey",@"Choose the key using the file picker again.");return;
    }
    if ([action isEqualToString:@"nativeAlert"]) {
        NSAlert *alert = [[NSAlert alloc] init];
        NSString *title = [body[@"Title"] isKindOfClass:[NSString class]] ? body[@"Title"] : @"SimplyPrint @ SoC";
        NSString *informativeText = [body[@"Message"] isKindOfClass:[NSString class]] ? body[@"Message"] : @"";
        NSString *style = [body[@"Style"] isKindOfClass:[NSString class]] ? body[@"Style"] : @"info";
        alert.messageText = title;
        alert.informativeText = informativeText;
        if ([style isEqualToString:@"error"]) alert.alertStyle = NSAlertStyleCritical;
        else if ([style isEqualToString:@"warning"]) alert.alertStyle = NSAlertStyleWarning;
        else alert.alertStyle = NSAlertStyleInformational;
        NSArray *buttons = [body[@"Buttons"] isKindOfClass:[NSArray class]] ? body[@"Buttons"] : @[];
        NSUInteger buttonCount = 0;
        for (id value in buttons) {
            if (![value isKindOfClass:[NSString class]] || [value length] == 0 || buttonCount == 3) continue;
            [alert addButtonWithTitle:value];
            buttonCount++;
        }
        if (buttonCount == 0) [alert addButtonWithTitle:@"OK"];
        [alert beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse response) {
            NSInteger index = response >= NSAlertFirstButtonReturn && response <= NSAlertThirdButtonReturn
                ? response - NSAlertFirstButtonReturn : -1;
            deliverNativeResponse(requestID, YES, @{ @"ButtonIndex": @(index) }, nil, nil);
        }];
        return;
    }
    if ([action isEqualToString:@"revealSecret"]) {
        NSString *secret = [body[@"Secret"] isKindOfClass:[NSString class]] ? body[@"Secret"] : @"";
        BOOL retrieveFromKeychain = [body[@"Retrieve"] boolValue];
        if (![secret isEqualToString:@"password"] && ![secret isEqualToString:@"keyPassphrase"]) {
            deliverNativeResponse(requestID, NO, nil, @"invalidSecret", @"That saved credential cannot be revealed.");
            return;
        }
        NSUInteger revision=self.accountRevision;
        LAContext *context = [[LAContext alloc] init];
        context.localizedFallbackTitle = @"Use Mac Password";
        NSError *availabilityError = nil;
        if (![context canEvaluatePolicy:LAPolicyDeviceOwnerAuthentication error:&availabilityError]) {
            deliverNativeResponse(requestID, NO, nil, @"localAuthUnavailable",
                @"Mac authentication is unavailable. Check that this Mac has an account password configured.");
            return;
        }
        NSString *reason = [secret isEqualToString:@"password"]
            ? @"Use Touch ID or your Mac password to reveal your saved SoC account password."
            : @"Use Touch ID or your Mac password to reveal your saved SSH key passphrase.";
        [context evaluatePolicy:LAPolicyDeviceOwnerAuthentication localizedReason:reason
            reply:^(BOOL success, NSError *error) {
                dispatch_async(dispatch_get_main_queue(), ^{
                    if (!success || revision != self.accountRevision) {
                        NSString *code = error.code == LAErrorUserCancel || error.code == LAErrorSystemCancel
                            ? @"authenticationCancelled" : @"localAuthFailed";
                        NSString *message = [code isEqualToString:@"authenticationCancelled"]
                            ? @"Authentication was cancelled."
                            : @"Mac authentication did not succeed. Try Touch ID or your Mac password again.";
                        deliverNativeResponse(requestID, NO, nil, code, message);
                        return;
                    }
                    if (retrieveFromKeychain) {
                        goRevealSecret((char *)requestID.UTF8String, (char *)secret.UTF8String);
                    } else {
                        deliverNativeResponse(requestID, YES, @{ @"Authorized": @YES }, nil, nil);
                    }
                });
            }];
        return;
    }
    if ([action isEqualToString:@"quitApp"]) {
        [NSApp terminate:nil];
        return;
    }
    if ([action isEqualToString:@"choosePDF"] || [action isEqualToString:@"chooseKey"]) {
        NSOpenPanel *panel = [NSOpenPanel openPanel];
        panel.canChooseFiles = YES;
        panel.canChooseDirectories = NO;
        panel.allowsMultipleSelection = NO;
        if ([action isEqualToString:@"choosePDF"]) {
            panel.allowedContentTypes = @[ UTTypePDF ];
            panel.message = @"Choose the PDF you want to send to a SoC printer.";
        } else {
            panel.message = @"Choose your SSH private key file. The key stays on this Mac.";
        }
        [panel beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse response) {
            if (response == NSModalResponseOK && panel.URL) {
                if ([action isEqualToString:@"choosePDF"]) {
                    dispatch_async(self.pdfQueue, ^{
                        NSError *error=nil;NSDictionary *data=[self.pdf openURL:panel.URL error:&error];
                        deliverNativeResponse(requestID,data!=nil,data,data?nil:@"invalidPDF",error.localizedDescription);
                    });
                } else {
                    [self.approvedKeys addObject:panel.URL.path];
                    deliverNativeResponse(requestID, YES, @{ @"Path": panel.URL.path ?: @"" }, nil, nil);
                }
            } else {
                deliverNativeResponse(requestID, YES, @{ @"Cancelled": @YES }, nil, nil);
            }
        }];
        return;
    }
    if([@[@"previewPDF",@"preparePDF",@"releaseDocument",@"submitPrint"] containsObject:action]) {
        dispatch_async(self.pdfQueue, ^{
            NSError *error=nil;NSDictionary *data=nil;
            if([action isEqualToString:@"previewPDF"])data=[self.pdf preview:body error:&error];
            else if([action isEqualToString:@"preparePDF"])data=[self.pdf prepare:body error:&error];
            else if([action isEqualToString:@"releaseDocument"]) {if([self.pdf release:body[@"Handle"] error:&error])data=@{@"Released":@YES};}
            else {
                data=[self.pdf submission:body error:&error];
                if(data){goHandleMessage((char *)jsonString(data).UTF8String);return;}
            }
            deliverNativeResponse(requestID,data!=nil,data,data?nil:@"pdfPreparationFailed",error.localizedDescription?:@"The document action failed.");
        });return;
    }
    if ([action isEqualToString:@"copyText"]) {
        NSString *text = [body[@"Text"] isKindOfClass:[NSString class]] ? body[@"Text"] : @"";
        NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
        [pasteboard clearContents];
        [pasteboard setString:text forType:NSPasteboardTypeString];
        deliverNativeResponse(requestID, YES, @{ @"Copied": @YES }, nil, nil);
        return;
    }
    if ([action isEqualToString:@"openURL"]) {
        NSString *rawURL = [body[@"URL"] isKindOfClass:[NSString class]] ? body[@"URL"] : @"";
        NSURL *url = [NSURL URLWithString:rawURL];
        NSString *host = url.host.lowercaseString;
        BOOL allowed = [url.scheme.lowercaseString isEqualToString:@"https"] &&
            ([host isEqualToString:@"comp.nus.edu.sg"] || [host hasSuffix:@".comp.nus.edu.sg"] ||
             [host isEqualToString:@"nus.edu.sg"] || [host hasSuffix:@".nus.edu.sg"] ||
             ([host isEqualToString:@"ppannawitt.github.io"] && [url.path hasPrefix:@"/print-soc/"]));
        if (allowed) {
            [[NSWorkspace sharedWorkspace] openURL:url];
            deliverNativeResponse(requestID, YES, @{ @"Opened": @YES }, nil, nil);
        } else {
            deliverNativeResponse(requestID, NO, nil, @"invalidURL", @"This help link could not be opened.");
        }
        return;
    }
    NSData *data = [NSJSONSerialization dataWithJSONObject:body options:NSJSONWritingFragmentsAllowed error:nil];
    if (!data) {
        deliverNativeResponse(requestID, NO, nil, @"invalidRequest", @"The app could not read that action.");
        return;
    }
    NSString *payload = [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
    goHandleMessage((char *)payload.UTF8String);
}

- (void)webView:(WKWebView *)webView decidePolicyForNavigationAction:(WKNavigationAction *)navigationAction decisionHandler:(void (^)(WKNavigationActionPolicy))decisionHandler {
    NSURL *url = navigationAction.request.URL;
    BOOL allowed=SPTrustedURL(url)&&navigationAction.targetFrame.isMainFrame;
    decisionHandler(allowed?WKNavigationActionPolicyAllow:WKNavigationActionPolicyCancel);
}

@end

void socprint_run(const char *assets) {
    @autoreleasepool {
        NSApplication *application = [NSApplication sharedApplication];
        retainedDelegate = [[SoCPrintDelegate alloc] init];
        retainedDelegate.resources = [NSJSONSerialization JSONObjectWithData:[[NSString stringWithUTF8String:assets ?: "{}"] dataUsingEncoding:NSUTF8StringEncoding] options:0 error:nil];
        application.delegate = retainedDelegate;
        [application run];
    }
}

void socprint_respond(const char *response) {
    NSString *json = [NSString stringWithUTF8String:response ?: "{}"] ?: @"{}";
    dispatch_async(dispatch_get_main_queue(), ^{
        WKWebView *webView = activeWebView;
        if (!webView || !SPTrustedURL(webView.URL)) return;
        // Only native-approved key paths can be sent back into subsequent requests.
        NSDictionary *reply=[NSJSONSerialization JSONObjectWithData:[json dataUsingEncoding:NSUTF8StringEncoding] options:0 error:nil];
        NSDictionary *data=[reply[@"Data"] isKindOfClass:NSDictionary.class]?reply[@"Data"]:nil;
        id path=data[@"KeyPath"];
        if([path isKindOfClass:NSString.class] && [path length]) [retainedDelegate.approvedKeys addObject:path];
        NSString *script = [NSString stringWithFormat:@"window.socprintReceive(%@);", json];
        [webView evaluateJavaScript:script completionHandler:nil];
    });
}
