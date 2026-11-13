// Shared Chromium launch options for every browser test.
//
// Set PLAYWRIGHT_CHROMIUM_EXECUTABLE when the pinned Playwright browsers are
// not installed (for example in a CI image that ships its own Chromium).
// E2E_CHROME is kept as an alias for older local scripts.
export function chromiumLaunchOptions() {
  const executablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE || process.env.E2E_CHROME || '';
  return executablePath ? { executablePath } : {};
}
