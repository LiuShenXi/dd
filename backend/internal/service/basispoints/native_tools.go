package basispoints

import (
	"fmt"
	"strings"
)

// Responses tools may encode a namespace separately from the base name. Resolve
// that exact identity before considering the legacy host display prefix. Never
// discard an explicit namespace or guess by a unique short-name/suffix match.
func (b *Bridge) directCatalogTool(native object) (tool, bool) {
	name := text(native["name"])
	if raw, present := native["namespace"]; present && raw != nil {
		namespace, valid := raw.(string)
		if !valid {
			return tool{}, false
		}
		if namespace != "" {
			if info, ok := b.tools[namespace+"."+name]; ok {
				return info, true
			}
			if info, ok := b.tools[name]; ok && (info.Namespace == namespace || namespace == "functions") {
				return info, true
			}
			if namespace != "functions" {
				return tool{}, false
			}
			// functions is also the BPS host's existing display namespace.
			info, ok := b.tools[strings.TrimPrefix(name, "functions.")]
			return info, ok
		}
	}
	if info, ok := b.tools[name]; ok {
		return info, true
	}
	info, ok := b.tools[strings.TrimPrefix(name, "functions.")]
	return info, ok
}

// Unknown names may contain caller/model data. Print only a fixed known-native
// label, structural facts and a stable digest, never arguments or arbitrary names.
func unsupportedNativeToolError(native object, reason string) error {
	name := text(native["name"])
	namespaceState := "absent"
	if raw, present := native["namespace"]; present && raw != nil {
		ns, ok := raw.(string)
		switch {
		case !ok:
			namespaceState = "invalid"
		case ns == "":
			namespaceState = "empty"
		default:
			namespaceState = "present"
		}
	}
	label := "unknown"
	candidate := strings.TrimPrefix(name, "functions.")
	switch candidate {
	case "search_workbook", "read_ranges", "read_range_image", "read_sheets_metadata",
		"list_skills", "read_skills", "update_plan", "request_user_input_basispoints",
		"list_connectors", "run_connector_action", "write_range", "format_range",
		"web.run", "web__run":
		label = candidate
	}
	identity := fingerprint([]any{native["namespace"], name})[:12]
	return fmt.Errorf("basispoints returned an unsupported native tool; no tool was executed (reason=%s; native_tool=%s; namespace=%s; name_bytes=%d; identity=%s)", reason, label, namespaceState, len(name), identity)
}
