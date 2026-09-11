#import "selection_toolbar_darwin.h"

@interface PopchatToolbarSurface : NSVisualEffectView
@end
@implementation PopchatToolbarSurface
- (BOOL)isFlipped { return YES; }
- (void)viewDidChangeEffectiveAppearance {
    [super viewDidChangeEffectiveAppearance];
    [self.effectiveAppearance performAsCurrentDrawingAppearance:^{
        self.layer.borderColor = [NSColor.separatorColor colorWithAlphaComponent:0.28].CGColor;
        self.layer.backgroundColor = [NSColor.windowBackgroundColor colorWithAlphaComponent:0.94].CGColor;
    }];
    self.needsDisplay = YES;
}
@end

@interface PopchatToolbarButton : NSButton
@property(retain) NSTrackingArea *hoverArea;
@property BOOL hovered;
@property(copy) NSString *symbolName;
@end
@implementation PopchatToolbarButton
- (BOOL)isFlipped { return YES; }
- (BOOL)acceptsFirstMouse:(NSEvent *)event { return YES; }
- (void)updateTrackingAreas {
    [super updateTrackingAreas];
    if (self.hoverArea) [self removeTrackingArea:self.hoverArea];
    self.hoverArea = [[[NSTrackingArea alloc] initWithRect:NSZeroRect
        options:NSTrackingMouseEnteredAndExited | NSTrackingActiveAlways | NSTrackingInVisibleRect
        owner:self userInfo:nil] autorelease];
    [self addTrackingArea:self.hoverArea];
}
- (void)mouseEntered:(NSEvent *)event { self.hovered = YES; self.needsDisplay = YES; }
- (void)mouseExited:(NSEvent *)event { self.hovered = NO; self.needsDisplay = YES; }
- (void)drawRect:(NSRect)dirtyRect {
    BOOL pressed = self.highlighted;
    if (self.hovered || pressed) {
        [[NSColor.controlAccentColor colorWithAlphaComponent:pressed ? 0.18 : 0.09] setFill];
        [[NSBezierPath bezierPathWithRoundedRect:NSInsetRect(self.bounds, 1, 1)
                                       xRadius:8 yRadius:8] fill];
    }
    NSColor *ink = self.enabled ? NSColor.labelColor : NSColor.disabledControlTextColor;
    NSColor *iconColor = self.hovered ? NSColor.controlAccentColor : NSColor.secondaryLabelColor;
    NSImage *symbol = [NSImage imageWithSystemSymbolName:self.symbolName accessibilityDescription:nil];
    NSImageSymbolConfiguration *style = [NSImageSymbolConfiguration configurationWithPointSize:14
                                                                                     weight:NSFontWeightMedium];
    style = [style configurationByApplyingConfiguration:
             [NSImageSymbolConfiguration configurationWithPaletteColors:@[iconColor]]];
    symbol = [symbol imageWithSymbolConfiguration:style];
    [symbol drawInRect:NSMakeRect(10, (NSHeight(self.bounds)-16)/2, 16, 16)
             fromRect:NSZeroRect operation:NSCompositingOperationSourceOver fraction:1
       respectFlipped:YES hints:nil];
    NSDictionary *attributes = @{NSFontAttributeName:[NSFont systemFontOfSize:13 weight:NSFontWeightMedium],
                                 NSForegroundColorAttributeName:ink};
    NSAttributedString *label = [[[NSAttributedString alloc] initWithString:self.title attributes:attributes] autorelease];
    NSRect labelRect = NSMakeRect(33, (NSHeight(self.bounds)-label.size.height)/2,
                                 MAX(0,NSWidth(self.bounds)-43), label.size.height);
    [label drawWithRect:labelRect options:NSStringDrawingUsesLineFragmentOrigin | NSStringDrawingTruncatesLastVisibleLine];
}
- (void)dealloc {
    [_hoverArea release];
    [_symbolName release];
    [super dealloc];
}
@end

NSView *popchatSelectionToolbar(NSArray<NSDictionary *> *buttons,
                               CGFloat maxWidth, id target, SEL action) {
    PopchatToolbarSurface *surface = [[[PopchatToolbarSurface alloc] initWithFrame:NSZeroRect] autorelease];
    surface.material = NSVisualEffectMaterialPopover;
    surface.blendingMode = NSVisualEffectBlendingModeBehindWindow;
    surface.state = NSVisualEffectStateActive;
    surface.wantsLayer = YES;
    surface.layer.cornerRadius = 14;
    surface.layer.masksToBounds = YES;
    surface.layer.borderWidth = 1;
    // Behind-window material and its window outline are composed outside the
    // view's CALayer. Give AppKit the same shape so both follow the round corners.
    NSImage *mask = [NSImage imageWithSize:NSMakeSize(29, 29) flipped:NO
        drawingHandler:^BOOL(NSRect rect) {
            [NSColor.blackColor setFill];
            [[NSBezierPath bezierPathWithRoundedRect:rect xRadius:14 yRadius:14] fill];
            return YES;
        }];
    mask.capInsets = NSEdgeInsetsMake(14, 14, 14, 14);
    mask.resizingMode = NSImageResizingModeStretch;
    surface.maskImage = mask;
    [surface viewDidChangeEffectiveAppearance];

    const CGFloat padding = 6, gap = 2, rowHeight = 34;
    CGFloat x = padding, y = padding, width = 0;
    maxWidth = MAX(100, maxWidth);
    for (NSUInteger index = 0; index < buttons.count; index++) {
        NSDictionary *item = buttons[index];
        NSString *name = item[@"name"] ?: @"";
        CGFloat textWidth = [name sizeWithAttributes:@{NSFontAttributeName:
                             [NSFont systemFontOfSize:13 weight:NSFontWeightMedium]}].width;
        CGFloat buttonWidth = MIN(MAX(ceil(textWidth)+44, 78), maxWidth-2*padding);
        if (x + buttonWidth + padding > maxWidth && x > padding) {
            width = MAX(width, x-gap+padding);
            x = padding;
            y += rowHeight+gap;
        }
        PopchatToolbarButton *button = [[[PopchatToolbarButton alloc]
                                         initWithFrame:NSMakeRect(x,y,buttonWidth,rowHeight)] autorelease];
        button.title = name;
        button.tag = index;
        button.target = target;
        button.action = action;
        button.bordered = NO;
        button.buttonType = NSButtonTypeMomentaryChange;
        button.focusRingType = NSFocusRingTypeNone;
        button.toolTip = name;
        button.accessibilityLabel = name;
        NSString *identifier = item[@"id"];
        button.symbolName = [identifier isEqualToString:@"translate"] ? @"character.bubble" :
                            [identifier isEqualToString:@"explain"] ? @"lightbulb" : @"sparkle";
        [surface addSubview:button];
        x += buttonWidth+gap;
    }
    width = MAX(width, x-gap+padding);
    surface.frame = NSMakeRect(0,0,width,buttons.count ? y+rowHeight+padding : 0);
    return surface;
}
