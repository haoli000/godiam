{{/*
Common labels
*/}}
{{- define "godiam.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}

{{/*
DRA selector labels
*/}}
{{- define "godiam.dra.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: dra
{{- end }}

{{/*
Server selector labels
*/}}
{{- define "godiam.server.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: server
{{- end }}

{{/*
Bench labels
*/}}
{{- define "godiam.bench.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: bench
{{- end }}

{{/*
Container image
*/}}
{{- define "godiam.image" -}}
{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}
{{- end }}

{{/*
DRA full name
*/}}
{{- define "godiam.dra.fullname" -}}
{{ .Release.Name }}-dra
{{- end }}

{{/*
Server full name
*/}}
{{- define "godiam.server.fullname" -}}
{{ .Release.Name }}-server
{{- end }}
