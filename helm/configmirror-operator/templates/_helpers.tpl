{{/*
Expand the name of the chart.
*/}}
{{- define "configmirror-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
Truncated to 63 chars (Kubernetes label value limit).
*/}}
{{- define "configmirror-operator.fullname" -}}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Chart label — includes the chart name and version.
*/}}
{{- define "configmirror-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to every resource — enables kubectl selectors and
Helm lifecycle management (upgrade, rollback, uninstall).
*/}}
{{- define "configmirror-operator.labels" -}}
helm.sh/chart: {{ include "configmirror-operator.chart" . }}
{{ include "configmirror-operator.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels — used by Deployment selector and Service selector.
Must be stable (never change after initial install).
*/}}
{{- define "configmirror-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "configmirror-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name — uses the value from values.yaml if set,
otherwise falls back to the fullname.
*/}}
{{- define "configmirror-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "configmirror-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
