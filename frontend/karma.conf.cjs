const path = require('path');

if (!process.env.CHROME_BIN) {
  try {
    // Uses Chromium bundled by Puppeteer, avoiding manual Chrome installation.
    process.env.CHROME_BIN = require('puppeteer').executablePath();
  } catch (error) {
    // Let Karma surface a clear launcher error if Puppeteer is unavailable.
    // eslint-disable-next-line no-console
    console.warn('Puppeteer executablePath() unavailable:', error);
  }
}

module.exports = function (config) {
  config.set({
    basePath: '',
    frameworks: ['jasmine'],
    plugins: [
      require('karma-jasmine'),
      require('karma-chrome-launcher'),
      require('karma-jasmine-html-reporter'),
      require('karma-coverage'),
    ],
    client: {
      jasmine: {},
      clearContext: false,
    },
    jasmineHtmlReporter: {
      suppressAll: true,
    },
    coverageReporter: {
      dir: path.join(__dirname, './coverage/frontend'),
      subdir: '.',
      reporters: [{ type: 'html' }, { type: 'text-summary' }],
    },
    reporters: ['progress', 'kjhtml'],
    browsers: ['ChromeHeadlessNoSandbox'],
    customLaunchers: {
      ChromeHeadlessNoSandbox: {
        base: 'ChromeHeadless',
        flags: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-gpu'],
      },
    },
    restartOnFileChange: true,
  });
};
