export interface TeiaOptions {
  /** Explicit frame/column aliases. JSON with optional nodes and edges sections. */
  frameMapping?: string;
  /** Optional teia API base, as the browser reaches it (e.g.
   * http://localhost:8480/api/v1/). When set, the panel streams from teia
   * directly instead of using the panel queries. */
  teiaUrl: string;
  /** The journey shown: a journey label value, key=value, or from:node.
   * May hold dashboard variables. Empty shows the whole map. */
  journey: string;
  /** The label naming journeys. Empty: the first of journey,
   * app.kubernetes.io/part-of and system the services carry. */
  journeyKey: string;
  /** Traffic and latency on every line. */
  values: boolean;
  /** Initial grouping label, e.g. team or k8s.namespace. */
  groupBy: string;
  /** Animate traffic along the edges. */
  flow: boolean;
}
