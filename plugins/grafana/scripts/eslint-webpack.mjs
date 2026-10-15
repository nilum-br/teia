import { ESLint } from 'eslint';
import path from 'node:path';

// Use ESLint's supported API directly: eslint-webpack-plugin pulls an unpatched
// braces parser through micromatch. Lint still runs on each development rebuild.
export class ESLintBuildPlugin {
  constructor(options = {}) {
    this.eslint = new ESLint({ cache: true, ...options });
    this.sourceDir = path.resolve(options.cwd ?? process.cwd(), 'src');
  }

  apply(compiler) {
    // Report after compilation so watch mode has collected file dependencies
    // and can recover when a lint error is fixed.
    compiler.hooks.afterCompile.tapPromise('TeiaESLint', async (compilation) => {
      compilation.contextDependencies.add(this.sourceDir);
      const results = await this.eslint.lintFiles(['src/**/*.{ts,tsx}']);
      for (const result of results) {
        compilation.fileDependencies.add(result.filePath);
      }
      const formatter = await this.eslint.loadFormatter('stylish');
      const output = formatter.format(results);
      if (results.some(result => result.errorCount > 0)) {
        compilation.errors.push(new Error(output));
      } else if (output) {
        compilation.warnings.push(new Error(output));
      }
    });
  }
}
