//
//  KBSearchResultView.m
//  Keybase
//
//  Created by Gabriel on 2/20/15.
//  Copyright (c) 2015 Gabriel Handford. All rights reserved.
//

#import "KBSearchResultView.h"
#import "KBUserImageView.h"

@interface KBSearchResultView ()
@property (nonatomic) KBSearchResult *searchResult;
@end

@implementation KBSearchResultView

- (void)setSearchResult:(KBSearchResult *)searchResult {
  _searchResult = searchResult;
  [self.titleLabel setText:searchResult.userName style:KBTextStyleDefault alignment:NSLeftTextAlignment lineBreakMode:NSLineBreakByTruncatingTail];
  [self.infoLabel setAttributedText:[self attributedStringForSearchResult:searchResult appearance:KBAppearance.currentAppearance] alignment:NSLeftTextAlignment lineBreakMode:NSLineBreakByTruncatingTail];
  [self.imageView kb_setUsername:searchResult.userName];
  self.imageSize = CGSizeMake(40, 40);
  [self setNeedsLayout];
}

- (NSMutableAttributedString *)attributedStringForSearchResult:(KBSearchResult *)searchResult appearance:(id<KBAppearance>)appearance {
  NSMutableArray *strings = [NSMutableArray array];
  if (searchResult.fullName) {
    [strings addObject:[[NSAttributedString alloc] initWithString:searchResult.fullName attributes:@{NSForegroundColorAttributeName:appearance.textColor, NSFontAttributeName: appearance.smallTextFont}]];
  }
  if (searchResult.twitter) {
    [strings addObject:[[NSAttributedString alloc] initWithString:NSStringWithFormat(@"@%@", searchResult.twitter) attributes:@{NSForegroundColorAttributeName:appearance.secondaryTextColor, NSFontAttributeName:appearance.smallTextFont}]];
  }

  return [KBText join:strings delimeter:[[NSAttributedString alloc] initWithString:@" • "]];
}

// Override
- (void)setBackgroundStyle:(NSBackgroundStyle)backgroundStyle {
  id<KBAppearance> appearance = (backgroundStyle == NSBackgroundStyleDark ? KBAppearance.darkAppearance : KBAppearance.lightAppearance);

  [self.titleLabel setStyle:KBTextStyleDefault options:0 appearance:appearance];
  [self.infoLabel setAttributedText:[self attributedStringForSearchResult:_searchResult appearance:appearance] alignment:NSLeftTextAlignment lineBreakMode:NSLineBreakByTruncatingTail];

  [self setNeedsLayout];
}

@end
