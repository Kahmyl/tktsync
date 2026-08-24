const emailPattern = /^[^\s@]+@[^\s@]+$/;

export function isEmailAddress(value: string) {
  return value.length <= 254 && emailPattern.test(value.trim());
}
