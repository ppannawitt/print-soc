#import "pdfengine.h"
#import <CoreGraphics/CoreGraphics.h>
#import <sys/stat.h>
#import <sys/file.h>
#import <fcntl.h>
#import <unistd.h>
#import <math.h>

static BOOL SPFail(NSError **error, NSString *message) {
    if(error) *error=[NSError errorWithDomain:@"PrintAtSoC" code:1 userInfo:@{NSLocalizedDescriptionKey:message}];
    return NO;
}
BOOL SPTrustedURL(NSURL *url) {
    return [url.scheme isEqualToString:@"socprint"] && [url.host isEqualToString:@"app"] &&
        [url.path isEqualToString:@"/index.html"] && !url.user && !url.password && !url.port && !url.query;
}
BOOL SPTrustedMessage(WKWebView *webView, WKFrameInfo *frame, WKWebView *expected) {
    return webView==expected && frame.isMainFrame && SPTrustedURL(frame.request.URL);
}
BOOL SPValidRequest(NSDictionary *body) {
    if(![body isKindOfClass:NSDictionary.class] || ![body[@"ID"] isKindOfClass:NSString.class] ||
       [body[@"ID"] length]>80 || ![body[@"Action"] isKindOfClass:NSString.class]) return NO;
    NSSet *strings=[NSSet setWithArray:@[@"ID",@"Action",@"Username",@"Password",@"KeyPath",@"KeyPassphrase",@"Passphrase",@"DocumentHandle",@"PreparedHandle",@"Handle",@"PrinterID",@"Queue",@"JobID",@"TermsVersion",@"Secret",@"Title",@"Message",@"Style",@"Text",@"URL"]];
    NSSet *booleans=[NSSet setWithArray:@[@"Forget",@"ClearKey",@"Retrieve"]];
    for(NSString *key in body) {
        id value=body[key];
        if([strings containsObject:key]) { if(![value isKindOfClass:NSString.class]||[value length]>16384)return NO; }
        else if([booleans containsObject:key]) {if(CFGetTypeID((__bridge CFTypeRef)value)!=CFBooleanGetTypeID())return NO;}
        else if([key isEqualToString:@"Options"]) {if(![value isKindOfClass:NSDictionary.class])return NO;}
        else if([key isEqualToString:@"Sheet"]) {if(![value isKindOfClass:NSNumber.class]||[value doubleValue]!=[value integerValue]||[value integerValue]<0)return NO;}
        else if([key isEqualToString:@"Buttons"]) {
            if(![value isKindOfClass:NSArray.class]||[value count]>3)return NO;
            for(id button in value) if(![button isKindOfClass:NSString.class]||[button length]>100)return NO;
        } else return NO; // Raw file paths are never a web API.
    }
    NSString *action=body[@"Action"];
    if(![@[@"initialize",@"acceptTerms",@"detectNetwork",@"saveCredentials",@"signIn",@"trustHost",@"cancelTrust",@"createKey",@"getPublicKey",@"useKey",@"submitPrint",@"listJobs",@"refreshJobs",@"queue",@"cancelJob",@"signOut",@"shutdown",@"choosePDF",@"chooseKey",@"previewPDF",@"preparePDF",@"releaseDocument",@"nativeAlert",@"revealSecret",@"quitApp",@"copyText",@"openURL"] containsObject:action])return NO;
    if([@[@"previewPDF",@"preparePDF"] containsObject:action]) {
        if(![body[@"DocumentHandle"] length] || !SPValidateOptions(body[@"Options"],nil))return NO;
        if([action isEqualToString:@"previewPDF"] && !body[@"Sheet"])return NO;
        if([action isEqualToString:@"preparePDF"] && (![body[@"PrinterID"] length]||![body[@"Queue"] length]))return NO;
    }
    if([action isEqualToString:@"submitPrint"] && ![body[@"PreparedHandle"] length])return NO;
    if([action isEqualToString:@"releaseDocument"] && ![body[@"Handle"] length])return NO;
    return YES;
}
NSArray<NSNumber *> *SPPageIndexes(NSString *range, NSUInteger count, NSError **error) {
    if(count==0||count>100000) {SPFail(error,@"Choose a PDF containing 1 to 100,000 pages.");return nil;}
    range=[range stringByTrimmingCharactersInSet:NSCharacterSet.whitespaceAndNewlineCharacterSet];
    NSMutableArray *indexes=[NSMutableArray array];
    if(range.length==0) {for(NSUInteger i=0;i<count;i++)[indexes addObject:@(i)];return indexes;}
    NSRegularExpression *re=[NSRegularExpression regularExpressionWithPattern:@"^([0-9]+)(?:\\s*-\\s*([0-9]+))?$" options:0 error:nil];
    for(NSString *raw in [range componentsSeparatedByString:@","]) {
        NSString *part=[raw stringByTrimmingCharactersInSet:NSCharacterSet.whitespaceAndNewlineCharacterSet];
        NSTextCheckingResult *m=[re firstMatchInString:part options:0 range:NSMakeRange(0,part.length)];
        if(!m){SPFail(error,@"Enter page numbers like 1-3, 5.");return nil;}
        NSInteger first=[[part substringWithRange:[m rangeAtIndex:1]] integerValue];
        NSInteger last=[m rangeAtIndex:2].location==NSNotFound?first:[[part substringWithRange:[m rangeAtIndex:2]] integerValue];
        if(first<1||last<first||(NSUInteger)last>count||indexes.count+(NSUInteger)(last-first+1)>100000) {
            SPFail(error,@"The page selection is outside this document or too large.");return nil;
        }
        for(NSInteger i=first;i<=last;i++)[indexes addObject:@(i-1)];
    }
    return indexes;
}
BOOL SPValidateOptions(NSDictionary *o, NSError **error) {
    if(![o isKindOfClass:NSDictionary.class]||o.count!=8) return SPFail(error,@"Check the print settings.");
    for(NSString *key in @[@"Paper",@"Orientation",@"ScaleMode",@"PageRange"]) if(![o[key] isKindOfClass:NSString.class])return SPFail(error,@"Check the print settings.");
    for(NSString *key in @[@"Copies",@"ScalePercent",@"PagesPerSheet"]) if(![o[key] isKindOfClass:NSNumber.class]||CFGetTypeID((__bridge CFTypeRef)o[key])==CFBooleanGetTypeID())return SPFail(error,@"Check numeric print settings.");
    if(!o[@"AutoRotate"]||CFGetTypeID((__bridge CFTypeRef)o[@"AutoRotate"])!=CFBooleanGetTypeID())return SPFail(error,@"Check automatic rotation.");
    if(![@[@"A4",@"A3"] containsObject:o[@"Paper"]]||![@[@"portrait",@"landscape"] containsObject:o[@"Orientation"]]||![@[@"fit",@"fill",@"percent"] containsObject:o[@"ScaleMode"]])return SPFail(error,@"Unsupported paper, orientation, or scaling.");
    double copies=[o[@"Copies"] doubleValue],scale=[o[@"ScalePercent"] doubleValue],n=[o[@"PagesPerSheet"] doubleValue];
    if(copies<1||copies>99||copies!=floor(copies)||!isfinite(scale)||scale<1||scale>400||![@[@1,@2,@4,@6,@9,@16] containsObject:@(n)]||[o[@"PageRange"] length]>16384)return SPFail(error,@"Check copies, scaling, layout, and pages.");
    return YES;
}
static CGSize SPPaper(NSDictionary *o) {
    CGSize s=[o[@"Paper"] isEqualToString:@"A3"]?CGSizeMake(841.8898,1190.5512):CGSizeMake(595.2756,841.8898);
    return [o[@"Orientation"] isEqualToString:@"landscape"]?CGSizeMake(s.height,s.width):s;
}
// The same vector drawing operation produces both preview and output PDF.
static BOOL SPDrawSheet(CGContextRef context, PDFDocument *source, NSArray *indexes, NSDictionary *o, NSUInteger sheet, NSError **error) {
    CGSize paper=SPPaper(o);NSUInteger n=[o[@"PagesPerSheet"] unsignedIntegerValue];
    NSUInteger cols=n==2?1:n==6?2:n==4?2:n==9?3:n==16?4:1,rows=(n+cols-1)/cols;
    if(paper.width>paper.height&&n==2){cols=2;rows=1;}
    CGFloat margin=18,gap=n==1?0:12,cw=(paper.width-2*margin-(cols-1)*gap)/cols,ch=(paper.height-2*margin-(rows-1)*gap)/rows;
    CGContextSetRGBFillColor(context,1,1,1,1);CGContextFillRect(context,CGRectMake(0,0,paper.width,paper.height));
    for(NSUInteger slot=0;slot<n;slot++) {
        NSUInteger index=sheet*n+slot;if(index>=indexes.count)break;
        PDFPage *page=[source pageAtIndex:[indexes[index] unsignedIntegerValue]];
        if(!page)return SPFail(error,@"One of the selected PDF pages is unreadable.");
        CGRect box=[page boundsForBox:kPDFDisplayBoxCropBox];
        NSInteger rotation=((page.rotation%360)+360)%360;
        if(box.size.width<=0||box.size.height<=0||!isfinite(box.size.width)||!isfinite(box.size.height))return SPFail(error,@"This PDF has invalid page dimensions.");
        CGFloat pw=rotation%180?box.size.height:box.size.width,ph=rotation%180?box.size.width:box.size.height;
        BOOL extra=[o[@"AutoRotate"] boolValue]&&((pw>ph)!=(cw>ch));
        if(extra){CGFloat t=pw;pw=ph;ph=t;}
        CGFloat factor=[o[@"ScaleMode"] isEqualToString:@"percent"]?[o[@"ScalePercent"] doubleValue]/100.0:
            [o[@"ScaleMode"] isEqualToString:@"fill"]?MAX(cw/pw,ch/ph):MIN(cw/pw,ch/ph);
        CGRect cell=CGRectMake(margin+(slot%cols)*(cw+gap),paper.height-margin-ch-(slot/cols)*(ch+gap),cw,ch);
        CGContextSaveGState(context);CGContextClipToRect(context,cell);
        CGContextTranslateCTM(context,CGRectGetMidX(cell),CGRectGetMidY(cell));CGContextScaleCTM(context,factor,factor);
        if(extra)CGContextRotateCTM(context,M_PI_2);
        CGFloat originalW=rotation%180?box.size.height:box.size.width,originalH=rotation%180?box.size.width:box.size.height;
        CGContextTranslateCTM(context,-originalW/2,-originalH/2);
        // PDFKit's drawing respects intrinsic rotation and annotations.
        [page transformContext:context forBox:kPDFDisplayBoxCropBox];
        [page drawWithBox:kPDFDisplayBoxCropBox toContext:context];
        CGContextRestoreGState(context);
    }
    return YES;
}
static size_t SPWrite(void *info,const void *buffer,size_t count) {
    int fd=*(int *)info;size_t total=0;
    while(total<count){ssize_t written=write(fd,(const char *)buffer+total,count-total);if(written<0&&errno==EINTR)continue;if(written<=0)break;total+=written;}
    return total;
}
@implementation SPPDFController {
    NSMutableDictionary *_documents,*_prepared;
    NSString *_directory;
    int _lock;
}
- (instancetype)init {
    if((self=[super init])) {
        _documents=[NSMutableDictionary dictionary];_prepared=[NSMutableDictionary dictionary];_lock=-1;
        NSString *root=[NSTemporaryDirectory() stringByAppendingPathComponent:@"print-at-soc-private"];
        if(mkdir(root.fileSystemRepresentation,0700)!=0&&errno!=EEXIST)return nil;
        struct stat st;if(lstat(root.fileSystemRepresentation,&st)!=0||!S_ISDIR(st.st_mode)||st.st_uid!=getuid()||chmod(root.fileSystemRepresentation,0700)!=0)return nil;
        // Recover only directories belonging to terminated instances. Live instances hold a flock.
        for(NSString *name in [[NSFileManager defaultManager] contentsOfDirectoryAtPath:root error:nil]) {
            if(![name hasPrefix:@"session-"])continue;
            NSString *path=[root stringByAppendingPathComponent:name];
            if(lstat(path.fileSystemRepresentation,&st)!=0||!S_ISDIR(st.st_mode)||st.st_uid!=getuid())continue;
            int fd=open([[path stringByAppendingPathComponent:@"owner.lock"] fileSystemRepresentation],O_RDWR|O_NOFOLLOW);
            if(fd>=0){if(flock(fd,LOCK_EX|LOCK_NB)==0)[[NSFileManager defaultManager] removeItemAtPath:path error:nil];close(fd);}
        }
        _directory=[root stringByAppendingPathComponent:[@"session-" stringByAppendingString:NSUUID.UUID.UUIDString]];
        if(mkdir(_directory.fileSystemRepresentation,0700)!=0)return nil;
        _lock=open([[_directory stringByAppendingPathComponent:@"owner.lock"] fileSystemRepresentation],O_CREAT|O_EXCL|O_RDWR|O_NOFOLLOW,0600);
        if(_lock<0||flock(_lock,LOCK_EX|LOCK_NB)!=0)return nil;
    }return self;
}
- (NSDictionary *)openURL:(NSURL *)url error:(NSError **)error {
    if(_documents.count>=4){SPFail(error,@"Close the previous document before choosing another PDF.");return nil;}
    int source=open(url.path.fileSystemRepresentation,O_RDONLY|O_NOFOLLOW);
    struct stat before,after;
    if(source<0||fstat(source,&before)!=0||!S_ISREG(before.st_mode)||before.st_size<5||before.st_size>1000000000){
        if(source>=0)close(source);SPFail(error,@"Choose a readable regular PDF no larger than 1 GB.");return nil;
    }
    // Snapshot once into an owned private file. Preview and submission use these exact bytes,
    // even if the user edits or removes the original after opening it.
    NSString *handle=NSUUID.UUID.UUIDString,*path=[_directory stringByAppendingPathComponent:[handle stringByAppendingString:@"-source.pdf"]];
    int target=open(path.fileSystemRepresentation,O_CREAT|O_EXCL|O_WRONLY|O_NOFOLLOW,0600);
    BOOL ok=target>=0;off_t total=0;char buffer[65536];
    while(ok){ssize_t count=read(source,buffer,sizeof(buffer));if(count<0&&errno==EINTR)continue;if(count<0){ok=NO;break;}if(count==0)break;
        total+=count;if(total>1000000000||SPWrite(&target,buffer,count)!=(size_t)count){ok=NO;break;}}
    if(fstat(source,&after)!=0||total!=before.st_size||before.st_size!=after.st_size||before.st_mtimespec.tv_sec!=after.st_mtimespec.tv_sec||before.st_mtimespec.tv_nsec!=after.st_mtimespec.tv_nsec)ok=NO;
    close(source);if(target>=0&&close(target)!=0)ok=NO;
    if(!ok){unlink(path.fileSystemRepresentation);SPFail(error,@"The PDF changed or could not be copied safely. Choose it again.");return nil;}
    PDFDocument *document=[[PDFDocument alloc] initWithURL:[NSURL fileURLWithPath:path]];
    if(!document||document.isLocked||document.pageCount==0||document.pageCount>100000){unlink(path.fileSystemRepresentation);SPFail(error,@"This PDF is locked, damaged, empty, or has too many pages.");return nil;}
    _documents[handle]=@{@"Document":document,@"Name":url.lastPathComponent,@"Size":@(total),@"FilePath":path};
    return @{@"DocumentHandle":handle,@"FileName":url.lastPathComponent,@"Size":@(total),@"PageCount":@(document.pageCount)};
}
- (NSArray *)indexes:(NSDictionary *)body document:(PDFDocument **)document error:(NSError **)error {
    NSDictionary *entry=_documents[body[@"DocumentHandle"]?:@""];
    if(!entry){SPFail(error,@"This document is no longer open. Choose it again.");return nil;}
    if(!SPValidateOptions(body[@"Options"],error))return nil;
    *document=entry[@"Document"];
    return SPPageIndexes(body[@"Options"][@"PageRange"],[*document pageCount],error);
}
- (NSDictionary *)preview:(NSDictionary *)body error:(NSError **)error {
    PDFDocument *document=nil;NSArray *indexes=[self indexes:body document:&document error:error];if(!indexes)return nil;
    NSUInteger n=[body[@"Options"][@"PagesPerSheet"] unsignedIntegerValue],sheet=[body[@"Sheet"] unsignedIntegerValue],count=(indexes.count+n-1)/n;
    if(sheet>=count){SPFail(error,@"That preview sheet does not exist.");return nil;}
    CGSize paper=SPPaper(body[@"Options"]);CGFloat scale=720/paper.height;
    NSBitmapImageRep *bitmap=[[NSBitmapImageRep alloc] initWithBitmapDataPlanes:nil pixelsWide:ceil(paper.width*scale) pixelsHigh:720 bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO colorSpaceName:NSDeviceRGBColorSpace bytesPerRow:0 bitsPerPixel:0];
    NSGraphicsContext *graphics=[NSGraphicsContext graphicsContextWithBitmapImageRep:bitmap];
    CGContextRef ctx=graphics.CGContext;CGContextScaleCTM(ctx,scale,scale);
    if(!SPDrawSheet(ctx,document,indexes,body[@"Options"],sheet,error))return nil;
    NSData *png=[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
    return @{@"Image":[@"data:image/png;base64," stringByAppendingString:[png base64EncodedStringWithOptions:0]],@"SheetCount":@(count),@"Sheet":@(sheet),@"Pages":[indexes subarrayWithRange:NSMakeRange(sheet*n,MIN(n,indexes.count-sheet*n))]};
}
- (NSDictionary *)prepare:(NSDictionary *)body error:(NSError **)error {
    PDFDocument *document=nil;NSArray *indexes=[self indexes:body document:&document error:error];if(!indexes)return nil;
    if(_prepared.count>=2){SPFail(error,@"Another document is already being prepared.");return nil;}
    NSString *handle=NSUUID.UUID.UUIDString,*path=[_directory stringByAppendingPathComponent:[handle stringByAppendingString:@".pdf"]];
    int fd=open(path.fileSystemRepresentation,O_CREAT|O_EXCL|O_WRONLY|O_NOFOLLOW,0600);
    if(fd<0){SPFail(error,@"Could not create a protected temporary PDF.");return nil;}
    CGDataConsumerCallbacks callbacks={SPWrite,NULL};CGDataConsumerRef consumer=CGDataConsumerCreate(&fd,&callbacks);
    CGSize paper=SPPaper(body[@"Options"]);CGRect box=CGRectMake(0,0,paper.width,paper.height);
    CGContextRef ctx=CGPDFContextCreate(consumer,&box,NULL);CGDataConsumerRelease(consumer);
    BOOL ok=ctx!=NULL;NSUInteger n=[body[@"Options"][@"PagesPerSheet"] unsignedIntegerValue],count=(indexes.count+n-1)/n;
    NSDate *deadline=[NSDate dateWithTimeIntervalSinceNow:120];
    for(NSUInteger sheet=0;ok&&sheet<count;sheet++) {
        @autoreleasepool {
            CGPDFContextBeginPage(ctx,NULL);ok=SPDrawSheet(ctx,document,indexes,body[@"Options"],sheet,error);CGPDFContextEndPage(ctx);
            struct stat st; if(fstat(fd,&st)!=0||st.st_size>1000000000||[deadline timeIntervalSinceNow]<0)ok=SPFail(error,@"The prepared PDF exceeded the size or preparation time limit.");
        }
    }
    if(ctx){CGPDFContextClose(ctx);CGContextRelease(ctx);}
    struct stat st;if(fstat(fd,&st)!=0||st.st_size<5||st.st_size>1000000000)ok=SPFail(error,@"The prepared PDF is empty or exceeds 1 GB.");
    if(close(fd)!=0)ok=SPFail(error,@"Could not finish writing the prepared PDF.");
    if(!ok){unlink(path.fileSystemRepresentation);return nil;}
    NSDictionary *entry=_documents[body[@"DocumentHandle"]?:@""];
    _prepared[handle]=@{@"FilePath":path,@"FileName":entry[@"Name"],@"PrintSettings":body[@"Options"],@"PrinterID":body[@"PrinterID"]?:@"",@"Queue":body[@"Queue"]?:@"",@"Username":body[@"Username"]?:@"",@"Consumed":@NO};
    return @{@"PreparedHandle":handle,@"SheetCount":@(count)};
}
- (NSDictionary *)submission:(NSDictionary *)body error:(NSError **)error {
    NSString *handle=body[@"PreparedHandle"]?:@"";NSDictionary *entry=_prepared[handle];
    if(!entry||[entry[@"Consumed"] boolValue]){SPFail(error,@"That print operation has expired or was already sent. Prepare it again.");return nil;}
    NSMutableDictionary *consumed=[entry mutableCopy];consumed[@"Consumed"]=@YES;_prepared[handle]=consumed;
    NSMutableDictionary *result=[entry mutableCopy];[result removeObjectForKey:@"Consumed"];
    result[@"ID"]=body[@"ID"];result[@"Action"]=@"submitPrint";
    result[@"Copies"]=entry[@"PrintSettings"][@"Copies"];result[@"PageRange"]=entry[@"PrintSettings"][@"PageRange"];
    return result;
}
- (BOOL)release:(NSString *)handle error:(NSError **)error {
    NSDictionary *entry=_prepared[handle?:@""];
    if(entry){if(unlink([entry[@"FilePath"] fileSystemRepresentation])!=0&&errno!=ENOENT)return SPFail(error,@"The protected temporary PDF could not be removed.");[_prepared removeObjectForKey:handle];}
    else if(_documents[handle]){
        NSString *path=_documents[handle][@"FilePath"];
        if(unlink(path.fileSystemRepresentation)!=0&&errno!=ENOENT)return SPFail(error,@"The protected source PDF could not be removed.");
        [_documents removeObjectForKey:handle];
    }
    else return SPFail(error,@"Unknown document handle.");
    return YES;
}
- (void)close {
    [_documents removeAllObjects];[_prepared removeAllObjects];
    if(_directory){NSError *error=nil;if(![[NSFileManager defaultManager] removeItemAtPath:_directory error:&error]&&error.code!=NSFileNoSuchFileError)NSLog(@"SimplyPrint @ SoC: temporary PDF cleanup failed; recovery will retry on next launch.");_directory=nil;}
    if(_lock>=0){close(_lock);_lock=-1;}
}
- (void)dealloc { [self close]; }
@end
