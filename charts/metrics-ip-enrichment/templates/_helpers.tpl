{{- define "metrics-ip-enrichment.fullname" -}}
metrics-ip-enrichment
{{- end }}

{{- define "metrics-ip-enrichment.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{ include "metrics-ip-enrichment.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "metrics-ip-enrichment.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
