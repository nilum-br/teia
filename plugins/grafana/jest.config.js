// force timezone to UTC to allow tests to work regardless of local timezone
// generally used by snapshots, but can affect specific tests
process.env.TZ = 'UTC';

const base = require('./.config/jest.config');

module.exports = {
  // Jest configuration provided by Grafana scaffolding
  ...base,
  moduleNameMapper: {
    ...base.moduleNameMapper,
    // The Grafana host provides @grafana/runtime; it is not installed (docs/dependency-status.md).
    '^@grafana/runtime$': '<rootDir>/src/test/grafana-runtime.ts',
  },
};
