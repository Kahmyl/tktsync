type DeviceNavigator = {
  userAgent?: string;
  userAgentData?: { mobile?: boolean };
};

/**
 * Camera scanning is intentionally limited to phones. A narrow browser window is
 * not enough evidence: laptops can be resized and generally do not have a rear
 * camera. Client hints are preferred, with a conservative user-agent fallback.
 */
export function isPhoneDevice(navigatorLike: DeviceNavigator) {
  if (navigatorLike.userAgentData?.mobile === true) return true;

  const userAgent = navigatorLike.userAgent ?? '';
  return /iPhone|iPod|Windows Phone|BlackBerry|Opera Mini|Android.+Mobile|Mobile.+Safari/i.test(
    userAgent,
  );
}

export function scannerGateLabel(phoneDevice: boolean) {
  return phoneDevice ? 'Phone scanner' : 'Manual entry';
}
