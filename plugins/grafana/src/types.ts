export interface TeiaOptions {
  /** Optional teia API base, as the browser reaches it (e.g.
   * http://localhost:8480/api/v1/). When set, the panel streams from teia
   * directly instead of using the panel queries. */
  teiaUrl: string;
  /** Initial grouping label, e.g. team or k8s.namespace. */
  groupBy: string;
  /** Animate traffic along the edges. */
  flow: boolean;
}
