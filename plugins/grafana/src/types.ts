export interface TeiaOptions {
  /** Optional teia API base (e.g. http://teia:8080/api/v1/). When set, the
   * panel streams from teia directly instead of using the panel queries. */
  teiaUrl: string;
  /** Initial grouping label, e.g. team or k8s.namespace. */
  groupBy: string;
  /** Animate traffic along the edges. */
  flow: boolean;
}
