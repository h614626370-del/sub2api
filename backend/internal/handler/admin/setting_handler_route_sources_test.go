package admin

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type routeSourcesGroupReader struct{}

func (routeSourcesGroupReader) GetByID(_ context.Context, id int64) (*service.Group, error) {
	switch id {
	case 10, 11:
		return &service.Group{ID: id, Platform: service.PlatformOpenAI, Status: service.StatusActive}, nil
	case 20:
		return &service.Group{ID: id, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSpecial}, nil
	}
	return nil, errors.New("missing group")
}

func TestSettingsRouteSourcesPartialAndAtomicSave(t *testing.T) {
	key := service.SettingKeyOpenAIAstraSourceGroupIDs
	sol := service.SettingKeyOpenAISolSourceGroupIDs
	target := service.SettingKeyOpenAIAstraGroupID
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		key: "[10]", sol: "[]", target: "20",
	})
	h.settingService.SetDefaultSubscriptionGroupReader(routeSourcesGroupReader{})
	for _, payload := range []map[string]any{{"site_name": "updated"}, {key: nil}, {sol: []int64{11}}} {
		rec := doUpdateSettings(t, h, payload, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, "[10]", repo.values[key])
		require.Equal(t, "20", repo.values[target])
	}
	rec := doUpdateSettings(t, h, map[string]any{key: []int64{11, 10, 11}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "[10,11]", repo.values[key])
	require.Contains(t, rec.Body.String(), `"openai_astra_source_group_ids":[10,11]`)
	rec = doUpdateSettings(t, h, map[string]any{key: []int64{999}, target: 0, sol: []int64{}}, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "999")
	require.Equal(t, "[10,11]", repo.values[key])
	require.Equal(t, "20", repo.values[target])
	require.Equal(t, "[11]", repo.values[sol])
	rec = doUpdateSettings(t, h, map[string]any{key: []int64{}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "[]", repo.values[key])
	require.Equal(t, "[11]", repo.values[sol])
	require.Contains(t, rec.Body.String(), `"openai_astra_source_group_ids":[]`)
}
