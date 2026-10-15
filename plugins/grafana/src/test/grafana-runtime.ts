// Tests stand in for the Grafana host, which provides @grafana/runtime to the
// panel at run time: jest.config.js maps the module here.
export const variables: Array<{ name: string }> = [];
export const locationService = { partial: jest.fn() };
export function getTemplateSrv() {
  return { getVariables: () => variables };
}
