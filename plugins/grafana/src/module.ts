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
          'Stream straight from teia (for example http://teia:8080/api/v1/) instead of the panel queries. teia must allow this Grafana origin (cors in teia.yaml).',
        defaultValue: '',
      })
  );
