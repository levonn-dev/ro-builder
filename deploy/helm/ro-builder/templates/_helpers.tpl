{{/*
Standard Helm name helpers. These match the style emitted by `helm create`
so anyone fluent in Helm can read the chart without surprises.
*/}}

{{- define "ro-builder.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "ro-builder.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "ro-builder.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels; applied to every resource. The component label distinguishes
api vs sidecar; pass it via `(dict "component" "api" "root" .)` when calling.
*/}}
{{- define "ro-builder.labels" -}}
helm.sh/chart: {{ include "ro-builder.chart" .root }}
{{ include "ro-builder.selectorLabels" . }}
{{- if .root.Chart.AppVersion }}
app.kubernetes.io/version: {{ .root.Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .root.Release.Service }}
{{- end -}}

{{- define "ro-builder.selectorLabels" -}}
app.kubernetes.io/name: {{ include "ro-builder.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{/*
Per-component resource names; keeps "api" and "sidecar" suffixes consistent
across Deployment, Service, ServiceAccount, etc.
*/}}
{{- define "ro-builder.apiName" -}}
{{- printf "%s-api" (include "ro-builder.fullname" .) -}}
{{- end -}}

{{- define "ro-builder.sidecarName" -}}
{{- printf "%s-sidecar" (include "ro-builder.fullname" .) -}}
{{- end -}}

{{- define "ro-builder.postgresName" -}}
{{- printf "%s-postgres" (include "ro-builder.fullname" .) -}}
{{- end -}}
