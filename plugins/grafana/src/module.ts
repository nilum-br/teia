import { PanelPlugin } from '@grafana/data';
import { TeiaOptions } from './types';
import { TeiaPanel } from './components/TeiaPanel';

export const plugin = new PanelPlugin<TeiaOptions>(TeiaPanel)
  .setNoPadding()
  .setPanelOptions((builder) =>
    builder
      .addTextInput({
        path: 'groupBy',
        name: 'Group by label',
        description: 'Initial grouping, for example team, system or k8s.namespace. Viewers can change it on the panel.',
        defaultValue: '',
      })
      .addBooleanSwitch({
        path: 'flow',
        name: 'Animate traffic',
        description: 'Dots travel along each communication at the speed of its traffic; red dots are errors.',
        defaultValue: true,
      })
      .addTextInput({
        path: 'teiaUrl',
        name: 'teia API URL (optional)',
        description:
          'Stream straight from teia instead of the panel queries, for example http://localhost:8480/api/v1/. The browser connects to it, so use an address viewers can reach, and allow the Grafana origin in teia.yaml (cors.allowedOrigins).',
        defaultValue: '',
      })
  );
