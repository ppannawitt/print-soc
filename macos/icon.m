#import <Cocoa/Cocoa.h>
#import <CoreText/CoreText.h>
#import <arpa/inet.h>

static NSColor *hexColor(uint32_t value) {
    return [NSColor colorWithSRGBRed:((value >> 16) & 0xff) / 255.0
                               green:((value >> 8) & 0xff) / 255.0
                                blue:(value & 0xff) / 255.0
                               alpha:1.0];
}

static NSPoint screenPoint(CGFloat x, CGFloat y) {
    return NSMakePoint(x, 1280.0 - y);
}

static NSRect screenRect(CGFloat x, CGFloat y, CGFloat width, CGFloat height) {
    return NSMakeRect(x, 1280.0 - y - height, width, height);
}

static void roundRect(CGFloat x, CGFloat y, CGFloat width, CGFloat height, CGFloat radius, NSColor *color) {
    [color setFill];
    [[NSBezierPath bezierPathWithRoundedRect:screenRect(x, y, width, height)
                                     xRadius:radius
                                     yRadius:radius] fill];
}

static void circle(CGFloat x, CGFloat y, CGFloat radius, NSColor *color) {
    [color setFill];
    [[NSBezierPath bezierPathWithOvalInRect:screenRect(x - radius, y - radius, radius * 2, radius * 2)] fill];
}

static void drawWordmark(void) {
    CTFontRef font = CTFontCreateWithName(CFSTR("Arial-BoldMT"), 98, NULL);
    NSColor *green = hexColor(0x1B6048);
    NSDictionary *attributes = @{
        (__bridge id)kCTFontAttributeName: (__bridge id)font,
        (__bridge id)kCTForegroundColorAttributeName: (__bridge id)green.CGColor
    };
    NSAttributedString *attributed = [[NSAttributedString alloc] initWithString:@"SoC" attributes:attributes];
    CTLineRef line = CTLineCreateWithAttributedString((__bridge CFAttributedStringRef)attributed);
    CGFloat width = (CGFloat)CTLineGetTypographicBounds(line, NULL, NULL, NULL);
    CGContextRef context = NSGraphicsContext.currentContext.CGContext;
    CGContextSetTextPosition(context, (1280.0 - width) / 2.0, 1280.0 - 368.0);
    CTLineDraw(line, context);
    CFRelease(line);
    CFRelease(font);
}

static void drawIcon(void) {
    roundRect(0, 0, 1280, 1280, 0, hexColor(0x151515));
    roundRect(79, 94, 1127, 1127, 255, hexColor(0x1B6048));

    NSBezierPath *sparkle = [NSBezierPath bezierPath];
    [sparkle moveToPoint:screenPoint(274, 244)];
    [sparkle curveToPoint:screenPoint(349, 319)
            controlPoint1:screenPoint(286, 294)
            controlPoint2:screenPoint(300, 307)];
    [sparkle curveToPoint:screenPoint(274, 394)
            controlPoint1:screenPoint(300, 331)
            controlPoint2:screenPoint(286, 344)];
    [sparkle curveToPoint:screenPoint(199, 319)
            controlPoint1:screenPoint(262, 344)
            controlPoint2:screenPoint(248, 331)];
    [sparkle curveToPoint:screenPoint(274, 244)
            controlPoint1:screenPoint(248, 307)
            controlPoint2:screenPoint(262, 294)];
    [sparkle closePath];
    [hexColor(0xFFD166) setFill];
    [sparkle fill];

    NSPoint terminalCenter = screenPoint(633, 410);
    [NSGraphicsContext saveGraphicsState];
    NSAffineTransform *terminalRotation = [NSAffineTransform transform];
    [terminalRotation translateXBy:terminalCenter.x yBy:terminalCenter.y];
    [terminalRotation rotateByDegrees:5.5];
    [terminalRotation translateXBy:-terminalCenter.x yBy:-terminalCenter.y];
    [terminalRotation concat];

    roundRect(420, 244, 426, 350, 34, NSColor.whiteColor);
    drawWordmark();
    NSBezierPath *prompt = [NSBezierPath bezierPath];
    [prompt moveToPoint:screenPoint(543, 419)];
    [prompt lineToPoint:screenPoint(595, 452)];
    [prompt lineToPoint:screenPoint(551, 496)];
    prompt.lineWidth = 20;
    prompt.lineCapStyle = NSLineCapStyleRound;
    prompt.lineJoinStyle = NSLineJoinStyleRound;
    [hexColor(0x1B6048) setStroke];
    [prompt stroke];
    NSBezierPath *cursor = [NSBezierPath bezierPath];
    [cursor moveToPoint:screenPoint(629, 486)];
    [cursor lineToPoint:screenPoint(715, 476)];
    cursor.lineWidth = 20;
    cursor.lineCapStyle = NSLineCapStyleRound;
    [cursor stroke];
    [NSGraphicsContext restoreGraphicsState];

    roundRect(248, 545, 789, 413, 109, hexColor(0xE5F4EB));
    circle(935, 619, 26, hexColor(0xFFD166));
    circle(492, 713, 41, hexColor(0x153F32));
    circle(503, 698, 13, NSColor.whiteColor);
    circle(792, 713, 41, hexColor(0x153F32));
    circle(803, 698, 13, NSColor.whiteColor);
    circle(409, 800, 34, hexColor(0xFFB1A3));
    circle(875, 800, 34, hexColor(0xFFB1A3));

    NSBezierPath *smile = [NSBezierPath bezierPath];
    [smile moveToPoint:screenPoint(559, 807)];
    [smile curveToPoint:screenPoint(727, 807)
          controlPoint1:screenPoint(590, 870)
          controlPoint2:screenPoint(696, 870)];
    smile.lineWidth = 21;
    smile.lineCapStyle = NSLineCapStyleRound;
    smile.lineJoinStyle = NSLineJoinStyleRound;
    [hexColor(0x153F32) setStroke];
    [smile stroke];

    roundRect(360, 898, 564, 34, 17, hexColor(0x1C6248));
    NSPoint paperCenter = screenPoint(644, 1019);
    [NSGraphicsContext saveGraphicsState];
    NSAffineTransform *paperRotation = [NSAffineTransform transform];
    [paperRotation translateXBy:paperCenter.x yBy:paperCenter.y];
    [paperRotation rotateByDegrees:-3.0];
    [paperRotation translateXBy:-paperCenter.x yBy:-paperCenter.y];
    [paperRotation concat];
    roundRect(419, 903, 450, 232, 24, NSColor.whiteColor);
    NSBezierPath *paperMark = [NSBezierPath bezierPath];
    [paperMark moveToPoint:screenPoint(482, 1038)];
    [paperMark lineToPoint:screenPoint(524, 1040)];
    paperMark.lineWidth = 22;
    [hexColor(0x9BCBB1) setStroke];
    [paperMark stroke];
    [NSGraphicsContext restoreGraphicsState];
}

int main(int argc, char **argv) {
    @autoreleasepool {
        if (argc != 4) return 1;
        NSString *folder = [NSString stringWithUTF8String:argv[1]];
        NSString *preview = [NSString stringWithUTF8String:argv[2]];
        NSString *icnsPath = [NSString stringWithUTF8String:argv[3]];
        [[NSFileManager defaultManager] createDirectoryAtPath:folder
                                  withIntermediateDirectories:YES
                                                   attributes:nil
                                                        error:nil];

        NSImage *image = [[NSImage alloc] initWithSize:NSMakeSize(1280, 1280)];
        [image lockFocus];
        drawIcon();
        [image unlockFocus];

        NSMutableData *chunks = [NSMutableData data];
        NSDictionary *chunkNames = @{@16:@"icp4", @32:@"icp5", @64:@"icp6",
                                     @128:@"ic07", @256:@"ic08", @512:@"ic09", @1024:@"ic10"};
        for (NSNumber *size in @[@16, @32, @64, @128, @256, @512, @1024]) {
            NSInteger dimension = size.integerValue;
            NSBitmapImageRep *bitmap = [[NSBitmapImageRep alloc] initWithBitmapDataPlanes:nil
                                                                              pixelsWide:dimension
                                                                              pixelsHigh:dimension
                                                                           bitsPerSample:8
                                                                         samplesPerPixel:4
                                                                                hasAlpha:YES
                                                                                isPlanar:NO
                                                                          colorSpaceName:NSDeviceRGBColorSpace
                                                                             bytesPerRow:0
                                                                            bitsPerPixel:0];
            [NSGraphicsContext saveGraphicsState];
            [NSGraphicsContext setCurrentContext:[NSGraphicsContext graphicsContextWithBitmapImageRep:bitmap]];
            NSGraphicsContext.currentContext.imageInterpolation = NSImageInterpolationHigh;
            [image drawInRect:NSMakeRect(0, 0, dimension, dimension)
                     fromRect:NSZeroRect
                    operation:NSCompositingOperationCopy
                     fraction:1];
            [NSGraphicsContext restoreGraphicsState];
            NSData *png = [bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
            [chunks appendData:[chunkNames[size] dataUsingEncoding:NSASCIIStringEncoding]];
            uint32_t length = htonl((uint32_t)png.length + 8);
            [chunks appendBytes:&length length:4];
            [chunks appendData:png];

            NSString *name = dimension == 1024 ? @"icon_512x512@2x.png"
                                                : [NSString stringWithFormat:@"icon_%ldx%ld.png", (long)dimension, (long)dimension];
            if (dimension == 16 || dimension == 32 || dimension == 128 || dimension == 256 || dimension == 512 || dimension == 1024) {
                [png writeToFile:[folder stringByAppendingPathComponent:name] atomically:YES];
            }
            if (dimension == 32 || dimension == 64 || dimension == 256 || dimension == 512 || dimension == 1024) {
                NSString *doubleName = [NSString stringWithFormat:@"icon_%ldx%ld@2x.png", (long)dimension / 2, (long)dimension / 2];
                [png writeToFile:[folder stringByAppendingPathComponent:doubleName] atomically:YES];
            }
            if (dimension == 1024) [png writeToFile:preview atomically:YES];
        }

        NSMutableData *icns = [NSMutableData dataWithBytes:"icns" length:4];
        uint32_t length = htonl((uint32_t)chunks.length + 8);
        [icns appendBytes:&length length:4];
        [icns appendData:chunks];
        return [icns writeToFile:icnsPath atomically:YES] ? 0 : 1;
    }
}
