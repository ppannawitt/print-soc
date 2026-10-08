#import "../cmd/socprint-macos/pdfengine.h"
#import <sys/stat.h>
#import <unistd.h>
#import <sys/file.h>
static void check(BOOL ok,NSString *message){if(!ok){fprintf(stderr,"FAIL: %s\n",message.UTF8String);exit(1);}}
static NSDictionary *options(NSString *range,NSString *paper,NSString *orientation,NSString *mode,NSNumber *n){return @{@"PageRange":range,@"Copies":@2,@"Paper":paper,@"Orientation":orientation,@"AutoRotate":@YES,@"ScaleMode":mode,@"ScalePercent":@100,@"PagesPerSheet":n};}
static PDFPage *page(CGSize size,NSInteger rotation){
    NSMutableData *data=[NSMutableData data];CGDataConsumerRef consumer=CGDataConsumerCreateWithCFData((__bridge CFMutableDataRef)data);CGRect box=CGRectMake(0,0,size.width,size.height);
    CGContextRef ctx=CGPDFContextCreate(consumer,&box,NULL);CGDataConsumerRelease(consumer);CGPDFContextBeginPage(ctx,NULL);
    CGContextSetRGBFillColor(ctx,1,0,0,1);CGContextFillRect(ctx,CGRectMake(0,0,size.width/2,size.height));
    CGContextSetRGBFillColor(ctx,0,0,1,1);CGContextFillRect(ctx,CGRectMake(size.width/2,0,size.width/2,size.height));
    CGPDFContextEndPage(ctx);CGPDFContextClose(ctx);CGContextRelease(ctx);
    PDFDocument *d=[[PDFDocument alloc] initWithData:data];PDFPage *p=[d pageAtIndex:0];p.rotation=rotation;return p;
}
int main(){@autoreleasepool{
    NSError *e=nil;check([SPPageIndexes(@"1-3, 5",5,&e) isEqual:@[@0,@1,@2,@4]],@"range selection");
    for(NSString *r in @[@"0",@"6",@"3-1",@"1,,2",@"wrong"])check(!SPPageIndexes(r,5,&e),@"invalid range rejected");
    check(!SPValidateOptions(@{},&e),@"missing options rejected without crashing");
    NSMutableDictionary *invalid=[options(@"",@"A4",@"portrait",@"fit",@1) mutableCopy];invalid[@"Copies"]=@100;check(!SPValidateOptions(invalid,&e),@"copies limit");
    invalid[@"Copies"]=@1;invalid[@"ScalePercent"]=@0;check(!SPValidateOptions(invalid,&e),@"scale limit");
    invalid[@"ScalePercent"]=@100;invalid[@"PagesPerSheet"]=@3;check(!SPValidateOptions(invalid,&e),@"layout validation");
    check(SPTrustedURL([NSURL URLWithString:@"socprint://app/index.html"]),@"private origin permitted");
    for(NSString *url in @[@"https://app/index.html",@"socprint://evil/index.html",@"socprint://app/model.js",@"file:///tmp/index.html",@"socprint://user@app/index.html",@"socprint://app/index.html?x=1"])check(!SPTrustedURL([NSURL URLWithString:url]),@"untrusted navigation rejected");
    check(!SPTrustedMessage(nil,nil,nil),@"untrusted bridge frame rejected");
    check(!SPValidRequest(@{@"ID":@"1",@"Action":@"submitPrint",@"FilePath":@"/etc/passwd"}),@"arbitrary paths rejected");
    check(!SPValidRequest(@{@"ID":@"1",@"Action":@"previewPDF",@"Options":@"bad"}),@"malformed request rejected");
    check(SPValidRequest(@{@"ID":@"1",@"Action":@"previewPDF",@"DocumentHandle":@"doc",@"Options":options(@"",@"A4",@"portrait",@"fit",@1),@"Sheet":@0}),@"valid preview request permitted");
    check(!SPValidRequest(@{@"ID":@"1",@"Action":@"registerKey"}),@"automatic registration action removed");
    check(SPValidRequest(@{@"ID":@"1",@"Action":@"getPublicKey"}),@"local public-key export allowed");
    check(!SPValidRequest(@{@"ID":@"1",@"Action":@"arbitraryAction"}),@"unknown action rejected");
    check(!SPValidRequest(@{@"ID":@"1",@"Action":@"submitPrint"}),@"missing prepared handle rejected");
    NSString *privateRoot=[NSTemporaryDirectory() stringByAppendingPathComponent:@"print-at-soc-private"];
    [[NSFileManager defaultManager] createDirectoryAtPath:privateRoot withIntermediateDirectories:YES attributes:@{NSFilePosixPermissions:@0700} error:nil];
    NSString *abandoned=[privateRoot stringByAppendingPathComponent:[@"session-test-" stringByAppendingString:NSUUID.UUID.UUIDString]];
    NSString *active=[privateRoot stringByAppendingPathComponent:[@"session-test-" stringByAppendingString:NSUUID.UUID.UUIDString]];
    for(NSString *session in @[abandoned,active]){[[NSFileManager defaultManager] createDirectoryAtPath:session withIntermediateDirectories:NO attributes:@{NSFilePosixPermissions:@0700} error:nil];int lock=open([[session stringByAppendingPathComponent:@"owner.lock"] fileSystemRepresentation],O_CREAT|O_RDWR,0600);close(lock);[@"fixture" writeToFile:[session stringByAppendingPathComponent:@"leftover.pdf"] atomically:NO encoding:NSUTF8StringEncoding error:nil];}
    int liveLock=open([[active stringByAppendingPathComponent:@"owner.lock"] fileSystemRepresentation],O_RDWR);check(flock(liveLock,LOCK_EX|LOCK_NB)==0,@"live fixture locked");
    NSString *directory=[NSTemporaryDirectory() stringByAppendingPathComponent:NSUUID.UUID.UUIDString];[[NSFileManager defaultManager] createDirectoryAtPath:directory withIntermediateDirectories:NO attributes:@{NSFilePosixPermissions:@0700} error:nil];
    NSString *path=[directory stringByAppendingPathComponent:@"fixture.pdf"];PDFDocument *source=[[PDFDocument alloc] init];
    [source insertPage:page(CGSizeMake(200,400),0) atIndex:0];[source insertPage:page(CGSizeMake(600,200),90) atIndex:1];[source insertPage:page(CGSizeMake(300,300),180) atIndex:2];check([source writeToFile:path],@"fixture created");
    if(getenv("SP_FIXTURE_EXPORT"))[source writeToFile:[NSString stringWithUTF8String:getenv("SP_FIXTURE_EXPORT")]];
    SPPDFController *engine=[[SPPDFController alloc] init];check(engine!=nil,@"private temp directory created");
    check(![[NSFileManager defaultManager] fileExistsAtPath:abandoned],@"crash leftovers recovered");
    check([[NSFileManager defaultManager] fileExistsAtPath:active],@"active instance not cleaned");close(liveLock);
    NSString *locked=[directory stringByAppendingPathComponent:@"locked.pdf"];
    check([source writeToFile:locked withOptions:@{PDFDocumentUserPasswordOption:@"fixture-password",PDFDocumentOwnerPasswordOption:@"fixture-owner"}],@"encrypted fixture written");
    check(![engine openURL:[NSURL fileURLWithPath:locked] error:&e],@"locked PDF rejected");
    NSDictionary *doc=[engine openURL:[NSURL fileURLWithPath:path] error:&e];check([doc[@"PageCount"] intValue]==3,@"mixed rotated document opened");
    check(unlink(path.fileSystemRepresentation)==0,@"original removed after snapshot");
    for(NSString *paper in @[@"A4",@"A3"])for(NSString *orientation in @[@"portrait",@"landscape"])for(NSString *mode in @[@"fit",@"fill",@"percent"])for(NSNumber *n in @[@1,@2,@4,@6,@9,@16]){
        NSDictionary *o=options(@"3,1-2",paper,orientation,mode,n);
        NSDictionary *body=@{@"DocumentHandle":doc[@"DocumentHandle"],@"Options":o,@"Sheet":@0,@"PrinterID":@"psc008",@"Queue":@"psc008"};
        NSDictionary *preview=[engine preview:body error:&e];check([preview[@"Image"] hasPrefix:@"data:image/png;base64,"],@"preview generated");
        NSData *previewBytes=[[NSData alloc] initWithBase64EncodedString:[preview[@"Image"] substringFromIndex:22] options:0];
        NSBitmapImageRep *previewBitmap=[[NSBitmapImageRep alloc] initWithData:previewBytes];
        NSUInteger colored=0;for(NSInteger y=0;y<previewBitmap.pixelsHigh;y+=8)for(NSInteger x=0;x<previewBitmap.pixelsWide;x+=8){NSColor *c=[[previewBitmap colorAtX:x y:y] colorUsingColorSpace:NSColorSpace.sRGBColorSpace];if(c.redComponent>.8&&c.blueComponent<.2||c.blueComponent>.8&&c.redComponent<.2)colored++;}
        check(colored>50,@"preview contains rendered page content");
        if(getenv("SP_PREVIEW_EXPORT")&&[paper isEqualToString:@"A4"]&&[orientation isEqualToString:@"portrait"]&&[mode isEqualToString:@"fit"]&&[n isEqual:@1])[previewBytes writeToFile:[NSString stringWithUTF8String:getenv("SP_PREVIEW_EXPORT")] atomically:YES];
        NSDictionary *prepared=[engine prepare:body error:&e];check(prepared!=nil,e.localizedDescription?:@"PDF prepared");
        NSDictionary *submission=[engine submission:@{@"PreparedHandle":prepared[@"PreparedHandle"],@"ID":@"test"} error:&e];check(submission!=nil,@"prepared handle resolved");
        check(![engine submission:@{@"PreparedHandle":prepared[@"PreparedHandle"],@"ID":@"repeat"} error:&e],@"duplicate submission rejected");
        PDFDocument *out=[[PDFDocument alloc] initWithURL:[NSURL fileURLWithPath:submission[@"FilePath"]]];check(out.pageCount==(3+[n intValue]-1)/[n intValue],@"output layout sheet count");
        CGRect bounds=[[out pageAtIndex:0] boundsForBox:kPDFDisplayBoxMediaBox];check((bounds.size.width>bounds.size.height)==[orientation isEqualToString:@"landscape"],@"output orientation");
        NSBitmapImageRep *outputBitmap=[[NSBitmapImageRep alloc] initWithBitmapDataPlanes:nil pixelsWide:previewBitmap.pixelsWide pixelsHigh:previewBitmap.pixelsHigh bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO colorSpaceName:NSDeviceRGBColorSpace bytesPerRow:0 bitsPerPixel:0];
        CGContextRef outputContext=[NSGraphicsContext graphicsContextWithBitmapImageRep:outputBitmap].CGContext;
        CGFloat outputScale=720/bounds.size.height;CGContextScaleCTM(outputContext,outputScale,outputScale);
        [[out pageAtIndex:0] transformContext:outputContext forBox:kPDFDisplayBoxMediaBox];[[out pageAtIndex:0] drawWithBox:kPDFDisplayBoxMediaBox toContext:outputContext];
        NSUInteger different=0,samples=0;
        for(NSInteger y=12;y<previewBitmap.pixelsHigh-12;y+=17)for(NSInteger x=12;x<previewBitmap.pixelsWide-12;x+=17){NSColor *a=[[previewBitmap colorAtX:x y:y] colorUsingColorSpace:NSColorSpace.sRGBColorSpace],*b=[[outputBitmap colorAtX:x y:y] colorUsingColorSpace:NSColorSpace.sRGBColorSpace];samples++;if(fabs(a.redComponent-b.redComponent)>.15||fabs(a.blueComponent-b.blueComponent)>.15)different++;}
        check(different<samples*.03,@"preview matches submitted PDF layout, rotation, scaling and clipping");
        struct stat st;check(stat([submission[@"FilePath"] fileSystemRepresentation],&st)==0&&(st.st_mode&0777)==0600,@"PDF private from creation");
        check([submission[@"Copies"] intValue]==2,@"copies preserved");
        check([engine release:prepared[@"PreparedHandle"] error:&e],@"prepared PDF cleaned");check(access([submission[@"FilePath"] fileSystemRepresentation],F_OK)!=0,@"file no longer present");
    }
    check(![engine preview:@{@"DocumentHandle":@"forged",@"Options":options(@"",@"A4",@"portrait",@"fit",@1),@"Sheet":@0} error:&e],@"invalid handle rejected");
    check([engine release:doc[@"DocumentHandle"] error:&e],@"source released");
    check(![engine openURL:[NSURL fileURLWithPath:@"/etc/hosts"] error:&e],@"malformed PDF rejected");
    NSString *large=[directory stringByAppendingPathComponent:@"large.pdf"];int fd=open(large.fileSystemRepresentation,O_CREAT|O_WRONLY,0600);ftruncate(fd,1000000001);close(fd);check(![engine openURL:[NSURL fileURLWithPath:large] error:&e],@"size limit enforced");
    [engine close];
    SPPDFController *recovery=[[SPPDFController alloc] init];check(![[NSFileManager defaultManager] fileExistsAtPath:active],@"released instance cleaned on next launch");[recovery close];
    [[NSFileManager defaultManager] removeItemAtPath:directory error:nil];puts("Native PDF and bridge security checks passed.");
}return 0;}
