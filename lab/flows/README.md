# Flows of the lab

`web`, `orders` and `payments` declare their calls. `catalog`, `recommendations` and the
workloads nobody instrumented have no file on purpose: teia generates one from observed traffic.
`orders` also declares a call to `notifications`, which is never deployed, so it shows as stale.
