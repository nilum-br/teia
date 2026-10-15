{{- define "teia.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "teia.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else if contains (include "teia.name" .) .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name (include "teia.name" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{- define "teia.selectorLabels" -}}
app.kubernetes.io/name: {{ include "teia.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "teia.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "teia.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "teia.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "teia.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/* The Secret holding the API token, or empty when the API is open. */}}
{{- define "teia.authSecret" -}}
{{- if .Values.auth.existingSecret }}
{{- .Values.auth.existingSecret }}
{{- else if .Values.auth.token }}
{{- printf "%s-auth" (include "teia.fullname" .) }}
{{- end }}
{{- end }}

{{- define "teia.flowsConfigMap" -}}
{{- default (printf "%s-flows" (include "teia.fullname" .)) .Values.flowsConfigMap }}
{{- end }}

{{/*
teia.yaml: the values' config, listening on the container port, reading the
mounted flows, and taking its token from TEIA_TOKEN (set from auth). teia
expands ${NAME} before parsing the YAML, so the reference is quoted: a token
is always read as a string.
*/}}
{{- define "teia.config" -}}
{{- $config := deepCopy .Values.config }}
{{- $_ := set $config "listen" ":8080" }}
{{- if .Values.persistence.enabled }}
{{- $history := default (dict) $config.history }}
{{- $_ := set $history "directory" "/var/lib/teia/history" }}
{{- $_ := set $config "history" $history }}
{{- end }}
{{- $auth := default (dict) $config.auth }}
{{- $_ := set $auth "token" "${TEIA_TOKEN:-}" }}
{{- $_ := set $config "auth" $auth }}
{{- $sources := default (dict) $config.sources }}
{{- $_ := set $sources "declared" (dict "paths" (list "/etc/teia/flows")) }}
{{- $_ := set $config "sources" $sources }}
{{- toYaml $config | replace "token: ${TEIA_TOKEN:-}" "token: \"${TEIA_TOKEN:-}\"" }}
{{- end }}
