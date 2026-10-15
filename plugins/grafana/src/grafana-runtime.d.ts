// The Grafana host provides @grafana/runtime to plugins (.config/bundler/externals.ts keeps it out of the
// bundle). The panel uses two of its functions; declaring them here keeps the package, and the advisories of
// the dependency tree it brings, out of the plugin's install. See docs/dependency-status.md.
declare module '@grafana/runtime' {
  export const locationService: {
    partial(query: Record<string, string | undefined>, replace?: boolean): void;
  };
  export function getTemplateSrv(): { getVariables(): Array<{ name: string }> };
}
