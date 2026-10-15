import { defineConfig } from 'eslint/config';
import js from '@eslint/js';
import react from '@eslint-react/eslint-plugin';
import hooks from 'eslint-plugin-react-hooks';
import ts from '@typescript-eslint/eslint-plugin';
import parser from '@typescript-eslint/parser';
import jsdoc from 'eslint-plugin-jsdoc';
import stylistic from '@stylistic/eslint-plugin';
import prettier from 'eslint-config-prettier';
import globals from 'globals';

export default defineConfig([
  {
    ignores: [
      // Managed scaffold templates; active configuration is owned here.
      '.config/**',
      '**/logs',
      '**/*.log',
      '**/npm-debug.log*',
      '**/yarn-debug.log*',
      '**/yarn-error.log*',
      '**/.pnpm-debug.log*',
      '**/node_modules/',
      '.yarn/cache',
      '.yarn/unplugged',
      '.yarn/build-state.yml',
      '.yarn/install-state.gz',
      '**/.pnp.*',
      '**/pids',
      '**/*.pid',
      '**/*.seed',
      '**/*.pid.lock',
      '**/lib-cov',
      '**/coverage',
      '**/dist/',
      '**/artifacts/',
      '**/work/',
      '**/ci/',
      'test-results/',
      'playwright-report/',
      'blob-report/',
      'playwright/.cache/',
      'playwright/.auth/',
      '**/.idea',
      '**/.eslintcache',
      // synced from internal/api/ui by scripts/sync-ui.mjs
      'src/teia/teia-ui.js',
    ],
  },
  js.configs.recommended,
  ...ts.configs['flat/recommended'],
  {
    languageOptions: { parser, globals: { ...globals.node, ...globals.browser, ...globals.jest } },
    plugins: { jsdoc, '@stylistic': stylistic },
    // Preserve the useful Grafana checks while replacing its React dependency,
    // whose peer range ends at ESLint 9. ESLint 10 removed new-parens from core.
    rules: {
      curly: 'error',
      'eol-last': 'error',
      eqeqeq: ['error', 'always', { null: 'ignore' }],
      'jsdoc/check-alignment': 'error',
      '@stylistic/new-parens': 'error',
      'no-caller': 'error',
      'no-console': ['error', { allow: ['error', 'log', 'warn', 'info'] }],
      'no-eval': 'error',
      'no-new-wrappers': 'error',
      'no-restricted-imports': ['error', 'moment'],
      'no-unused-labels': 'error',
      'no-var': 'error',
      radix: 'error',
      'no-duplicate-imports': 'error',
      '@typescript-eslint/no-unused-expressions': ['error', { allowShortCircuit: true, allowTernary: true }],
      '@typescript-eslint/array-type': ['error', { default: 'array-simple' }],
      '@typescript-eslint/naming-convention': ['error', { selector: 'interface', format: ['PascalCase'], custom: { regex: '^I[A-Z]', match: false } }],
      '@typescript-eslint/consistent-type-assertions': 'error',
      '@typescript-eslint/no-inferrable-types': 'error',
      '@typescript-eslint/no-namespace': ['error', { allowDeclarations: false }],
      '@typescript-eslint/triple-slash-reference': 'error',
    },
  },
  {
    files: ['*.config.js', '.prettierrc.js'],
    rules: { '@typescript-eslint/no-require-imports': 'off' },
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    extends: [react.configs['recommended-typescript'], hooks.configs.flat.recommended],
    languageOptions: { parserOptions: { project: './tsconfig.json', tsconfigRootDir: import.meta.dirname } },
    rules: { '@typescript-eslint/no-deprecated': 'warn', 'react-hooks/exhaustive-deps': 'error' },
  },
  prettier,
]);
