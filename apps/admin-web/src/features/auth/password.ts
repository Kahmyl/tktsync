export const minimumPasswordLength = 12;

export function passwordValidation(password: string, confirmation: string) {
  if (password.length < minimumPasswordLength) {
    return `Use at least ${minimumPasswordLength} characters.`;
  }
  if (password.length > 128) {
    return 'Use no more than 128 characters.';
  }
  if (password !== confirmation) {
    return 'The passwords do not match.';
  }
  return '';
}
