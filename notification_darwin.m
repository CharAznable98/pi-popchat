#import <AppKit/AppKit.h>
#import <UserNotifications/UserNotifications.h>
extern void popchatNotificationClicked(char *sid);
extern void popchatNotificationError(char *message);
@interface PopchatNotificationDelegate : NSObject <UNUserNotificationCenterDelegate>
@end
@implementation PopchatNotificationDelegate
- (void)userNotificationCenter:(UNUserNotificationCenter *)center didReceiveNotificationResponse:(UNNotificationResponse *)response withCompletionHandler:(void (^)(void))completionHandler {
 NSString *sid=response.notification.request.content.userInfo[@"sessionID"];
 if (sid && [response.actionIdentifier isEqualToString:UNNotificationDefaultActionIdentifier]) popchatNotificationClicked((char *)sid.UTF8String);
 completionHandler();
}
- (void)userNotificationCenter:(UNUserNotificationCenter *)center willPresentNotification:(UNNotification *)notification withCompletionHandler:(void (^)(UNNotificationPresentationOptions))completionHandler {
 completionHandler(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionSound | UNNotificationPresentationOptionList);
}
@end
static PopchatNotificationDelegate *notificationDelegate;
void popchatSetupNotifications(void) {
 @autoreleasepool {
  if (![[NSBundle mainBundle] bundleIdentifier]) return;
  notificationDelegate=[[PopchatNotificationDelegate alloc] init];
  UNUserNotificationCenter *center=[UNUserNotificationCenter currentNotificationCenter];center.delegate=notificationDelegate;
  [center requestAuthorizationWithOptions:UNAuthorizationOptionAlert|UNAuthorizationOptionSound completionHandler:^(BOOL granted,NSError *error){
   if(error) popchatNotificationError((char *)error.localizedDescription.UTF8String);
   else if(!granted) popchatNotificationError("系统通知未获授权；对话仍可正常使用");
  }];
 }
}
void popchatSendNotification(const char *sid,const char *title,const char *body) {
 @autoreleasepool {
  if (!notificationDelegate) return;
  UNMutableNotificationContent *content=[[UNMutableNotificationContent alloc] init];
  content.title=[NSString stringWithUTF8String:title];content.body=[NSString stringWithUTF8String:body];
  content.userInfo=@{@"sessionID":[NSString stringWithUTF8String:sid]};content.sound=[UNNotificationSound defaultSound];
  UNNotificationRequest *request=[UNNotificationRequest requestWithIdentifier:[NSString stringWithFormat:@"popchat-%s",sid] content:content trigger:nil];
  [[UNUserNotificationCenter currentNotificationCenter] addNotificationRequest:request withCompletionHandler:^(NSError *error){if(error)popchatNotificationError((char *)error.localizedDescription.UTF8String);}];
  [content release];
 }
}
