package yaml

const (
	defaultIndent         = "  "
	singleLineKeyFormat   = "%s: "
	singleLineValueFormat = "{{ \"%s\" }}"
	multilineKeyFormat    = "%s:"
	multilineValueFormat  = "{{- include \"%s\" | nindent %d }}"
	withMixedFormat       = `{{- with %s }}
%s:
  {{- toYaml . | nindent %d }}
{{- end }}`
)
