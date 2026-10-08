#import <Cocoa/Cocoa.h>
#import <PDFKit/PDFKit.h>
#import <WebKit/WebKit.h>

NSArray<NSNumber *> *SPPageIndexes(NSString *range, NSUInteger count, NSError **error);
BOOL SPValidateOptions(NSDictionary *options, NSError **error);
BOOL SPTrustedURL(NSURL *url);
BOOL SPTrustedMessage(WKWebView *webView, WKFrameInfo *frame, WKWebView *expected);
BOOL SPValidRequest(NSDictionary *body);

@interface SPPDFController : NSObject
- (NSDictionary *)openURL:(NSURL *)url error:(NSError **)error;
- (NSDictionary *)preview:(NSDictionary *)body error:(NSError **)error;
- (NSDictionary *)prepare:(NSDictionary *)body error:(NSError **)error;
- (NSDictionary *)submission:(NSDictionary *)body error:(NSError **)error;
- (BOOL)release:(NSString *)handle error:(NSError **)error;
- (void)close;
@end
