#import <Cocoa/Cocoa.h>
#import <arpa/inet.h>
static void roundRect(NSRect rect,CGFloat radius,NSColor *color){[color setFill];[[NSBezierPath bezierPathWithRoundedRect:rect xRadius:radius yRadius:radius] fill];}
int main(int argc,char **argv){@autoreleasepool{
    if(argc!=4)return 1;NSString *folder=[NSString stringWithUTF8String:argv[1]],*preview=[NSString stringWithUTF8String:argv[2]];
    [[NSFileManager defaultManager] createDirectoryAtPath:folder withIntermediateDirectories:YES attributes:nil error:nil];
    NSImage *image=[[NSImage alloc] initWithSize:NSMakeSize(1024,1024)];[image lockFocus];
    roundRect(NSMakeRect(48,48,928,928),210,[NSColor colorWithSRGBRed:.09 green:.36 blue:.25 alpha:1]);
    NSBezierPath *cloud=[NSBezierPath bezierPath];[cloud moveToPoint:NSMakePoint(266,570)];
    [cloud curveToPoint:NSMakePoint(278,790) controlPoint1:NSMakePoint(140,595) controlPoint2:NSMakePoint(159,786)];
    [cloud curveToPoint:NSMakePoint(625,801) controlPoint1:NSMakePoint(317,969) controlPoint2:NSMakePoint(565,971)];
    [cloud curveToPoint:NSMakePoint(737,565) controlPoint1:NSMakePoint(820,865) controlPoint2:NSMakePoint(876,598)];
    cloud.lineWidth=52;cloud.lineCapStyle=NSLineCapStyleRound;[[NSColor colorWithSRGBRed:.93 green:.98 blue:.95 alpha:1] setStroke];[cloud stroke];
    NSColor *white=[NSColor colorWithSRGBRed:.96 green:.98 blue:.97 alpha:1];
    roundRect(NSMakeRect(345,462,334,208),22,white);roundRect(NSMakeRect(239,296,546,284),68,white);
    roundRect(NSMakeRect(325,162,374,236),28,NSColor.whiteColor);
    roundRect(NSMakeRect(390,277,244,24),12,[NSColor colorWithSRGBRed:.60 green:.78 blue:.69 alpha:1]);
    roundRect(NSMakeRect(390,216,175,24),12,[NSColor colorWithSRGBRed:.60 green:.78 blue:.69 alpha:1]);
    [[NSColor colorWithSRGBRed:.20 green:.58 blue:.42 alpha:1] setFill];[[NSBezierPath bezierPathWithOvalInRect:NSMakeRect(691,472,37,37)] fill];
    [image unlockFocus];
    NSMutableData *chunks=[NSMutableData data];
    NSDictionary *chunkNames=@{@16:@"icp4",@32:@"icp5",@64:@"icp6",@128:@"ic07",@256:@"ic08",@512:@"ic09",@1024:@"ic10"};
    for(NSNumber *size in @[@16,@32,@64,@128,@256,@512,@1024]){
        NSInteger n=size.integerValue;NSBitmapImageRep *bitmap=[[NSBitmapImageRep alloc] initWithBitmapDataPlanes:nil pixelsWide:n pixelsHigh:n bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO colorSpaceName:NSDeviceRGBColorSpace bytesPerRow:0 bitsPerPixel:0];
        [NSGraphicsContext saveGraphicsState];[NSGraphicsContext setCurrentContext:[NSGraphicsContext graphicsContextWithBitmapImageRep:bitmap]];
        NSGraphicsContext.currentContext.imageInterpolation=NSImageInterpolationHigh;
        [image drawInRect:NSMakeRect(0,0,n,n) fromRect:NSZeroRect operation:NSCompositingOperationCopy fraction:1];[NSGraphicsContext restoreGraphicsState];
        NSData *data=[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        [chunks appendData:[chunkNames[size] dataUsingEncoding:NSASCIIStringEncoding]];uint32_t length=htonl((uint32_t)data.length+8);[chunks appendBytes:&length length:4];[chunks appendData:data];
        NSString *name=n==1024?@"icon_512x512@2x.png":[NSString stringWithFormat:@"icon_%ldx%ld.png",(long)n,(long)n];
        if(n==16||n==32||n==128||n==256||n==512||n==1024){if(![data writeToFile:[folder stringByAppendingPathComponent:name] atomically:YES])return 1;}
        if(n==32||n==64||n==256||n==512||n==1024){NSString *doubleName=[NSString stringWithFormat:@"icon_%ldx%ld@2x.png",(long)n/2,(long)n/2];[data writeToFile:[folder stringByAppendingPathComponent:doubleName] atomically:YES];}
        if(n==1024)[data writeToFile:preview atomically:YES];
    }
    NSMutableData *icns=[NSMutableData dataWithBytes:"icns" length:4];uint32_t length=htonl((uint32_t)chunks.length+8);[icns appendBytes:&length length:4];[icns appendData:chunks];if(![icns writeToFile:[NSString stringWithUTF8String:argv[3]] atomically:YES])return 1;
}return 0;}
