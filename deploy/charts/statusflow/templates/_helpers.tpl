{{- define "sf.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sf.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "sf.labels" -}}
app.kubernetes.io/name: {{ include "sf.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: statusflow
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{/* Stable selector labels — MUST NOT include the chart version (selectors are
immutable; a version bump would otherwise break `helm upgrade`). Pass a
"component" via dict to scope to api/worker/web. */}}
{{- define "sf.selectorLabels" -}}
app.kubernetes.io/name: {{ include "sf.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "sf.serviceAccountName" -}}
{{- if .Values.serviceAccount.name -}}
{{- .Values.serviceAccount.name -}}
{{- else -}}
{{- include "sf.fullname" . -}}
{{- end -}}
{{- end -}}

{{- define "sf.image" -}}
{{- $reg := .root.Values.images.registry -}}
{{- if $reg -}}
{{- printf "%s/%s:%s" $reg .img.repository .img.tag -}}
{{- else -}}
{{- printf "%s:%s" .img.repository .img.tag -}}
{{- end -}}
{{- end -}}

{{/* Component names */}}
{{- define "sf.api.fullname" -}}{{ printf "%s-api" (include "sf.fullname" .) }}{{- end -}}
{{- define "sf.worker.fullname" -}}{{ printf "%s-worker" (include "sf.fullname" .) }}{{- end -}}
{{- define "sf.web.fullname" -}}{{ printf "%s-web" (include "sf.fullname" .) }}{{- end -}}

{{/* Secret names */}}
{{- define "sf.dbSecretName" -}}
{{- if .Values.db.secret.existingSecret -}}{{ .Values.db.secret.existingSecret }}{{- else -}}{{ printf "%s-db" (include "sf.fullname" .) }}{{- end -}}
{{- end -}}
{{- define "sf.migrateSecretName" -}}
{{- if .Values.migrate.secret.existingSecret -}}{{ .Values.migrate.secret.existingSecret }}{{- else -}}{{ printf "%s-migrate" (include "sf.fullname" .) }}{{- end -}}
{{- end -}}
{{- define "sf.appSecretName" -}}
{{- if .Values.secrets.existingSecret -}}{{ .Values.secrets.existingSecret }}{{- else -}}{{ printf "%s-app" (include "sf.fullname" .) }}{{- end -}}
{{- end -}}

{{/* DSNs assembled from db.* (dev). app_user => api/worker; superuser => migrate Job ONLY. */}}
{{- define "sf.appDSN" -}}
postgres://{{ .Values.db.appUser }}:{{ .Values.db.appUserPassword }}@{{ .Values.db.host }}:{{ .Values.db.port }}/{{ .Values.db.name }}?sslmode={{ .Values.db.sslmode }}
{{- end -}}
{{- define "sf.migrateDSN" -}}
postgres://{{ .Values.db.superuser }}:{{ .Values.db.superuserPassword }}@{{ .Values.db.host }}:{{ .Values.db.port }}/{{ .Values.db.name }}?sslmode={{ .Values.db.sslmode }}
{{- end -}}

{{/* Internal API URL the BFF forwards to (server-side only). */}}
{{- define "sf.internalApiUrl" -}}
{{- if .Values.web.internalApiUrl -}}{{ .Values.web.internalApiUrl }}{{- else -}}http://{{ include "sf.api.fullname" . }}:{{ .Values.api.port }}{{- end -}}
{{- end -}}
