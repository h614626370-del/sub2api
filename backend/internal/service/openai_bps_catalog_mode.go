package service

import (
	"bytes"
	"encoding/json"
	"strings"
)

// A mixed pool must advertise the intersection, not the capabilities of the
// account which happened to supply the upstream catalog. Availability/cooldown
// changes must not widen capabilities; callers pass persistent catalog members.
func codexModelMayUseBPS(platform, model string, accounts []Account, group *Group, routes []CompositeModelRoute, routesAvailable bool) bool {
	if platform == PlatformOpenAIBPS {
		return true
	}
	publicModel := model
	if platform == PlatformComposite {
		var ok bool
		platform, model, ok = resolveCodexCompositeModelTarget(model, accounts, routes, routesAvailable)
		if !ok {
			return false
		}
		if platform == PlatformOpenAIBPS {
			return true
		}
	}
	if platform != PlatformOpenAI {
		return false
	}
	routed := codexModelRoutingAccountIDs(group, publicModel)
	explicit := false
	for i := range accounts {
		a := &accounts[i]
		if a.Platform == platform && (len(routed) == 0 || containsInt64(routed, a.ID)) && codexExplicitModelMappingClaims(*a, model) {
			explicit = true
		}
	}
	for i := range accounts {
		a := &accounts[i]
		if a.Platform != platform || (len(routed) > 0 && !containsInt64(routed, a.ID)) {
			continue
		}
		if explicit && !codexExplicitModelMappingClaims(*a, model) {
			continue
		}
		if a.IsModelSupported(model) && a.UsesOpenAIBPS(model) {
			return true
		}
	}
	return false
}

// Apply after both upstream and operator catalogs are merged, before computing
// the client ETag. Never mutate a cached upstream catalog or a non-BPS entry.
func applyBPSCatalogForAccounts(body []byte, platform string, accounts []Account, group *Group, routes []CompositeModelRoute, routesAvailable bool) ([]byte, bool, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, false, err
	}
	var models []json.RawMessage
	if err := json.Unmarshal(envelope["models"], &models); err != nil {
		return nil, false, err
	}
	changed := false
	for i, raw := range models {
		var model map[string]any
		if err := bpsDecode(raw, &model); err != nil || model == nil {
			continue // Preserve unrelated opaque entries, as the catalog merger does.
		}
		id := strings.TrimSpace(stringValue(model["slug"]))
		if id == "" || !codexModelMayUseBPS(platform, id, accounts, group, routes, routesAvailable) {
			continue
		}
		before, _ := json.Marshal(model)
		applyOpenAIBPSModelCapabilities(model)
		after, err := json.Marshal(model)
		if err != nil {
			return nil, false, err
		}
		// Normalize nested structs to the same JSON representation as a decoded
		// catalog; field ordering alone must not invalidate the client ETag.
		var normalized any
		if err := bpsDecode(after, &normalized); err != nil {
			return nil, false, err
		}
		after, err = json.Marshal(normalized)
		if err != nil {
			return nil, false, err
		}
		if !bytes.Equal(before, after) {
			models[i] = after
			changed = true
		}
	}
	if !changed {
		return body, false, nil
	}
	encoded, err := json.Marshal(models)
	if err != nil {
		return nil, false, err
	}
	envelope["models"] = encoded
	updated, err := json.Marshal(envelope)
	return updated, true, err
}
