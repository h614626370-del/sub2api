package service

import (
	"context"
	"fmt"
	"sort"
)

// A directory is cached only for an explicit client session. Inferring a session
// from identical user text must not grant tools to an unrelated conversation.
func (s *OpenAIGatewayService) bpsPrepareTools(ctx context.Context, account *Account, r *bpsRequest, input []any) ([]any, error) {
	if r.Compact {
		r.Tools = map[string]bpsTool{}
		return nil, nil
	}
	var catalog []any
	declared := false
	if raw, exists := r.Source["tools"]; exists {
		if err := bpsCollectTools(raw, "", r.Tools, &catalog, 0); err != nil {
			return nil, err
		}
		declared = len(r.Tools) > 0
	}
	for index, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok || item["type"] != "additional_tools" {
			continue
		}
		if item["role"] != "developer" {
			return nil, bpsInvalid(fmt.Sprintf("input[%d].additional_tools must have role developer", index))
		}
		tools, ok := item["tools"].([]any)
		if !ok {
			return nil, bpsInvalid(fmt.Sprintf("input[%d].additional_tools.tools must be an array", index))
		}
		if err := bpsCollectTools(tools, "", r.Tools, &catalog, 0); err != nil {
			return nil, err
		}
		declared = true // An explicit empty Lite directory revokes the old one.
	}
	cacheKey := bpsStateKey(r.Scope, account, "tools", "directory-v1")
	cacheable := r.ToolSessionExplicit
	if cacheable && !declared {
		store, err := s.bpsStateStore()
		if err != nil {
			return nil, err
		}
		data, err := store.BPSGet(ctx, cacheKey)
		if err != nil {
			return nil, bpsUnavailable()
		}
		if len(data) > 0 {
			var tools any
			if bpsDecode(data, &tools) != nil {
				return nil, bpsExpired()
			}
			if err := bpsCollectTools(tools, "", r.Tools, &catalog, 0); err != nil {
				return nil, bpsExpired()
			}
		}
	}
	// Persist declarations, not the tool_choice subset. Preserve namespace identity.
	names := make([]string, 0, len(r.Tools))
	for name := range r.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	definitions := make([]any, 0, len(names))
	for _, name := range names {
		tool := r.Tools[name]
		var definition any = tool.Spec
		if tool.Namespace != "" {
			definition = map[string]any{"type": "namespace", "name": tool.Namespace, "tools": []any{tool.Spec}}
		}
		definitions = append(definitions, definition)
	}
	choice := r.Source["tool_choice"]
	if selection, ok := choice.(map[string]any); ok {
		name := stringValue(selection["name"])
		if nested, ok := selection["function"].(map[string]any); ok {
			if name != "" && name != stringValue(nested["name"]) {
				return nil, bpsInvalid("Conflicting tool_choice names")
			}
			name = stringValue(nested["name"])
		}
		if ns := stringValue(selection["namespace"]); ns != "" {
			name = ns + "." + name
		}
		tool, found := r.Tools[name]
		if !found {
			return nil, bpsInvalid("tool_choice must name a declared client tool")
		}
		r.Tools = map[string]bpsTool{name: tool}
		r.RequireTool = true
	} else {
		if choice != nil && choice != "auto" && choice != "required" && choice != "none" && choice != "" {
			return nil, bpsInvalid("Unsupported tool_choice")
		}
		r.RequireTool = choice == "required"
		if choice == "none" {
			r.Tools = map[string]bpsTool{}
		}
	}
	if r.RequireTool && len(r.Tools) == 0 {
		return nil, bpsInvalid("tool_choice requires at least one client tool")
	}
	if cacheable && declared {
		if err := s.bpsPut(ctx, cacheKey, definitions); err != nil {
			return nil, err
		}
	}
	catalog = nil
	for _, name := range names {
		if tool, ok := r.Tools[name]; ok {
			if err := bpsCollectTools([]any{tool.Spec}, tool.Namespace, map[string]bpsTool{}, &catalog, 0); err != nil {
				return nil, err
			}
		}
	}
	return catalog, nil
}
