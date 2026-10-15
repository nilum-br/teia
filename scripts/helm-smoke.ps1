# Run only against a disposable kind cluster created for candidate validation.
# Never discovers a current context or reads a user's Kubernetes credentials.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Kubeconfig,
    [string]$Context = 'kind-teia-candidate',
    [string]$Helm = 'helm',
    [string]$Kubectl = 'kubectl',
    [string]$Image = 'teia-lab/teia:dev',
    [string]$TestImage = 'busybox:1.38.0',
    [switch]$History
)
$ErrorActionPreference = 'Stop'
if ($Context -notmatch '^kind-teia-candidate(?:-[a-z0-9-]+)?$') { throw 'Use a disposable kind-teia-candidate context' }
$Kubeconfig = (Resolve-Path -LiteralPath $Kubeconfig).Path
$repo = Split-Path -Parent $PSScriptRoot
$namespace = 'teia-smoke-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
$values = Join-Path ([IO.Path]::GetTempPath()) ($namespace + '.yaml')
$tagSeparator = $Image.LastIndexOf(':')
if ($tagSeparator -le $Image.LastIndexOf('/')) { throw 'Image needs an explicit tag' }
$imageRepository = $Image.Substring(0, $tagSeparator)
$imageTag = $Image.Substring($tagSeparator + 1)
$historyArgs = @()
if ($History) { $historyArgs = @('--set', 'persistence.enabled=true') }

function Kube {
    $Command = $args
    $result = & $Kubectl --kubeconfig $Kubeconfig --context $Context @Command
    if ($LASTEXITCODE -ne 0) { throw ('kubectl failed: ' + $Command[0]) }
    return $result
}
function HelmCall {
    $Command = $args
    & $Helm --kubeconfig $Kubeconfig --kube-context $Context @Command
    if ($LASTEXITCODE -ne 0) {
        if ($Command[0] -eq 'test') {
            try { Kube -n $namespace logs teia-test } catch { }
        }
        throw ('helm failed: ' + $Command[0])
    }
}
function ApplyObject($Object) {
    $Object | ConvertTo-Json -Depth 30 -Compress | & $Kubectl --kubeconfig $Kubeconfig --context $Context apply -f - | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'kubectl apply failed' }
}
function ReadAPI([string]$Path) {
    if ($Path -notmatch '^/api/v1/[A-Za-z0-9/._-]+$') { throw 'Unexpected API path' }
    $command = 'wget -T 10 -t 1 -qO- --header=Authorization:\ Bearer\ $TEIA_TOKEN http://teia:8080' + $Path
    $json = Kube -n $namespace exec probe '--' sh -c $command
    return (($json -join "`n") | ConvertFrom-Json)
}
function CheckAPI {
    # Wait for Service endpoints to catch up after replacing the single replica.
    for ($attempt = 0; $attempt -lt 4; $attempt++) {
        $anonymous = Kube -n $namespace exec probe '--' sh -c 'wget -T 5 -t 1 -S -O /dev/null http://teia:8080/api/v1/snapshot 2>&1 || true'
        if (($anonymous -join "`n") -match '401 Unauthorized') { break }
        if (($anonymous -join "`n") -match 'HTTP/1.1 200') { throw 'Anonymous API access was accepted' }
        Start-Sleep -Seconds 2
    }
    if (($anonymous -join "`n") -notmatch '401 Unauthorized') { throw ('Anonymous API check failed: ' + ($anonymous -join ' ')) }
    $snapshot = ReadAPI '/api/v1/snapshot'
    if (@($snapshot.sources | Where-Object { $_.name -eq 'kubernetes' -and !$_.error }).Count -ne 1) {
        throw 'Kubernetes inventory did not succeed with the mounted account credential'
    }
    if (@($snapshot.nodes | Where-Object id -eq 'candidate-client').Count -ne 1) { throw 'Declared fixture did not load' }
}

try {
    Kube get namespace kube-system -o name | Out-Null
    Kube create namespace $namespace | Out-Null
    $tokenBytes = New-Object byte[] 32
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($tokenBytes) } finally { $rng.Dispose() }
    $token = ([BitConverter]::ToString($tokenBytes)).Replace('-', '').ToLowerInvariant()
    ApplyObject @{ apiVersion='v1'; kind='Secret'; metadata=@{name='candidate-auth'; namespace=$namespace}; type='Opaque'; stringData=@{token=$token} }
    $token = $null
    @"
image:
  repository: $imageRepository
  tag: '$imageTag'
  pullPolicy: Never
auth:
  existingSecret: candidate-auth
config:
  interval: 2s
flows:
  candidate.yaml: |
    apiVersion: teia.nilum.io/v1alpha1
    kind: Flows
    metadata:
      name: candidate-client
    spec:
      owner: component:default/candidate-client
      egress:
        - to: component:default/candidate-db
          protocol: postgres
test:
  image: $TestImage
"@ | Set-Content -LiteralPath $values -Encoding UTF8
    HelmCall install teia (Join-Path $repo 'charts/teia') -n $namespace -f $values @historyArgs --wait --timeout 180s
    HelmCall test teia -n $namespace --timeout 120s
    ApplyObject @{
        apiVersion='v1'; kind='Pod'; metadata=@{name='probe'; namespace=$namespace}
        spec=@{automountServiceAccountToken=$false; restartPolicy='Never'; containers=@(@{
            name='probe'; image=$TestImage; imagePullPolicy='Never'; command=@('sleep','600')
            env=@(@{name='TEIA_TOKEN';valueFrom=@{secretKeyRef=@{name='candidate-auth';key='token'}}})
        })}
    }
    Kube -n $namespace wait --for=condition=Ready pod/probe --timeout=90s | Out-Null
    CheckAPI
    Write-Output 'PASS: fresh install, health hook, anonymous 401, authenticated graph and Kubernetes inventory'
    if ($History) {
        $stored = ReadAPI '/api/v1/history'
        if (!$stored.enabled -or !$stored.snapshots.Count) { throw 'History did not store a snapshot on the PVC' }
        $historyPath = '/api/v1/history/' + $stored.snapshots[0].id
        $before = ReadAPI $historyPath
        if ($before.timeMode -ne 'historical') { throw 'Stored snapshot was presented as current state' }
        $beforeJSON = $before | ConvertTo-Json -Depth 100 -Compress
    }

    # Supply both the account and its RBAC outside Helm, then remove the chart-owned versions.
    ApplyObject @{apiVersion='v1';kind='ServiceAccount';metadata=@{name='external-reader';namespace=$namespace};automountServiceAccountToken=$false}
    ApplyObject @{
        apiVersion='rbac.authorization.k8s.io/v1';kind='ClusterRole';metadata=@{name=$namespace}
        rules=@(@{apiGroups=@('');resources=@('pods','services','namespaces');verbs=@('get','list','watch')})
    }
    ApplyObject @{
        apiVersion='rbac.authorization.k8s.io/v1';kind='ClusterRoleBinding';metadata=@{name=$namespace}
        roleRef=@{apiGroup='rbac.authorization.k8s.io';kind='ClusterRole';name=$namespace}
        subjects=@(@{kind='ServiceAccount';name='external-reader';namespace=$namespace})
    }
    HelmCall upgrade teia (Join-Path $repo 'charts/teia') -n $namespace -f $values @historyArgs --set serviceAccount.create=false --set serviceAccount.name=external-reader --set rbac.create=false --wait --timeout 180s
    $deployment = (Kube -n $namespace get deployment teia -o json | Out-String | ConvertFrom-Json)
    if ($deployment.spec.template.spec.serviceAccountName -ne 'external-reader' -or $deployment.spec.template.spec.automountServiceAccountToken -ne $true) { throw 'External account token mounting regressed' }
    CheckAPI
    HelmCall test teia -n $namespace --timeout 120s
    Write-Output 'PASS: upgrade preserves auth and inventory with externally managed ServiceAccount/RBAC'
    if ($History) {
        $after = ReadAPI $historyPath
        if (($after | ConvertTo-Json -Depth 100 -Compress) -ne $beforeJSON) { throw 'Historical snapshot changed or disappeared after pod replacement' }
        Write-Output 'PASS: historical snapshot survives pod replacement on PVC without changing its content'
    }
} finally {
    # Only names created by this invocation; the cluster remains available for inspection.
    try { & $Helm --kubeconfig $Kubeconfig --kube-context $Context uninstall teia -n $namespace --ignore-not-found --wait --timeout 60s 2>$null | Out-Null } catch { }
    try { & $Kubectl --kubeconfig $Kubeconfig --context $Context delete namespace $namespace --ignore-not-found --wait=false 2>$null | Out-Null } catch { }
    try { & $Kubectl --kubeconfig $Kubeconfig --context $Context delete clusterrole,clusterrolebinding $namespace --ignore-not-found 2>$null | Out-Null } catch { }
    Remove-Item -LiteralPath $values -ErrorAction SilentlyContinue
}
