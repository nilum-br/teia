import assert from 'node:assert/strict';
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';
import { test } from 'node:test';
import webpack from 'webpack';
import { ESLintBuildPlugin } from './eslint-webpack.mjs';

const require = createRequire(import.meta.url);
const { loadNycConfig } = require('@istanbuljs/load-nyc-config');

test('Jest coverage config loads YAML 5, inheritance and normalizes options', async () => {
  const cwd = await mkdtemp(path.join(tmpdir(), 'teia-nyc-'));
  try {
    await writeFile(path.join(cwd, 'package.json'), '{"private":true}');
    await writeFile(path.join(cwd, 'base.yml'), 'all: true\ninclude: src/**/*.ts\nreporter: [text, json]\n');
    await writeFile(path.join(cwd, '.nycrc.yaml'), 'extends: ./base.yml\ncheck-coverage: true\nbranches: 80\nexclude:\n  - "**/*.test.ts"\n');
    const config = await loadNycConfig({ cwd });
    assert.equal(config.all, true);
    assert.equal(config.checkCoverage, true);
    assert.equal(config.branches, 80);
    assert.deepEqual(config.include, ['src/**/*.ts']);
    assert.deepEqual(config.exclude, ['**/*.test.ts']);
    assert.deepEqual(config.reporter, ['text', 'json']);
    // YAML configuration must remain data, never executable JavaScript tags.
    await writeFile(path.join(cwd, '.nycrc.yaml'), 'value: !!js/function "function () { return 1; }"\n');
    await assert.rejects(loadNycConfig({ cwd }), /unknown (?:scalar )?tag/i);
  } finally {
    await rm(cwd, { recursive: true, force: true });
  }
});

test('Webpack watch recovers from lint errors in unbundled and newly added files', { timeout: 15000 }, async (t) => {
  const cwd = await mkdtemp(path.join(tmpdir(), 'teia-lint-'));
  const entry = path.join(cwd, 'src', 'index.ts');
  const unbundled = path.join(cwd, 'src', 'unbundled.ts');
  let watching;
  try {
    await mkdir(path.dirname(entry));
    await writeFile(entry, 'export const value = 1;\n');
    await writeFile(unbundled, 'debugger; export const unused = 1;\n');
    const compiler = webpack({
      mode: 'development', context: cwd, entry,
      output: { path: path.join(cwd, 'dist') },
      plugins: [new ESLintBuildPlugin({
        cwd, cache: false, overrideConfigFile: true,
        overrideConfig: [{ files: ['**/*.ts'], rules: { 'no-debugger': 'error' } }],
      })],
    });
    await new Promise((resolve, reject) => {
      t.signal.addEventListener('abort', () => reject(t.signal.reason), { once: true });
      let phase = 0;
      watching = compiler.watch({ aggregateTimeout: 30 }, async (err, stats) => {
        if (err) { reject(err); return; }
        try {
          if (phase === 0) {
            assert.equal(stats.hasErrors(), true);
            assert.match(stats.toString({ all: false, errors: true }), /no-debugger/);
            phase = 1;
            await writeFile(unbundled, 'export const unused = 1;\n');
          } else if (phase === 1) {
            assert.equal(stats.hasErrors(), false);
            phase = 2;
            await writeFile(path.join(cwd, 'src', 'added.ts'), 'debugger; export const added = 1;\n');
          } else {
            assert.equal(stats.hasErrors(), true);
            assert.match(stats.toString({ all: false, errors: true }), /added\.ts/);
            resolve();
          }
        } catch (error) { reject(error); }
      });
    });
  } finally {
    if (watching) {
      await new Promise((resolve, reject) => watching.close(err => err ? reject(err) : resolve()));
    }
    await rm(cwd, { recursive: true, force: true });
  }
});
