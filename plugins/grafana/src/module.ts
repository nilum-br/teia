import { PanelPlugin } from '@grafana/data';
import { TeiaOptions } from './types';
import { TeiaPanel } from './components/TeiaPanel';

export const plugin = new PanelPlugin<TeiaOptions>(TeiaPanel)
  .setNoPadding()
  .setPanelOptions((builder) =>
    builder
      .addTextInput({
        path: 'journey',
        name: 'Journey',
        description:
          'Show one journey instead of the whole map: a value of the journey label (checkout), any label (team=payments), or what a service depends on (from:orders). Dashboard variables work, such as ${journey}.',
        defaultValue: '',
      })
      .addTextInput({
        path: 'journeyKey',
        name: 'Journey label',
        description:
          'The label that says which journeys a service takes part in. Empty: the first one your services already carry of journey, app.kubernetes.io/part-of and system.',
        defaultValue: '',
      })
      .addTextInput({
        path: 'groupBy',
        name: 'Group by label',
        description: 'Initial grouping, for example team, system or k8s.namespace. Viewers can change it on the panel.',
        defaultValue: '',
      })
      .addBooleanSwitch({
        path: 'values',
        name: 'Values on lines',
        description: 'Traffic and p95 latency on every line; an amber or red line shows why instead.',
        defaultValue: true,
      })
      .addBooleanSwitch({
        path: 'flow',
        name: 'Animate traffic',
        description: 'Dots travel along each communication at the speed of its traffic; red dots are errors.',
        defaultValue: true,
      })
      .addTextInput({
        path: 'frameMapping',
        name: 'Column mapping (JSON)',
        description: 'Optional nodes/edges column aliases, also configurable in the panel setup. Rates must be requests/s, errors fractions 0–1 and latency milliseconds; convert units in the query.',
        defaultValue: '',
      })
      .addTextInput({
        path: 'teiaUrl',
        name: 'teia API URL (optional)',
        description:
          'Stream straight from teia instead of the panel queries, for example http://localhost:8480/api/v1/. The browser connects to it, so use an address viewers can reach, and allow the Grafana origin in teia.yaml (cors.allowedOrigins).',
        defaultValue: '',
      })
  );
