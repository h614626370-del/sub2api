package service

import (
	"context"
	"encoding/json"
	"errors"
)

const SettingKeySubscriptionConversion = "subscription_conversion"
const SubscriptionStatusConverted = "converted"
const OrderStatusConverted = "CONVERTED"

type SubscriptionConversionSettings struct {
	Enabled bool `json:"enabled"`
}

func parseConversionSettings(raw string) (*SubscriptionConversionSettings, error) {
	cfg := &SubscriptionConversionSettings{}
	if raw == "" {
		return cfg, nil
	}
	err := json.Unmarshal([]byte(raw), cfg)
	return cfg, err
}
func conversionEnabled(raw string) bool {
	cfg, err := parseConversionSettings(raw)
	return err == nil && cfg.Enabled
}
func (s *SettingService) GetSubscriptionConversionSettings(ctx context.Context) (*SubscriptionConversionSettings, error) {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeySubscriptionConversion)
	if errors.Is(err, ErrSettingNotFound) {
		raw, err = "", nil
	}
	if err != nil {
		return nil, err
	}
	return parseConversionSettings(raw)
}
func (s *SettingService) SetSubscriptionConversionSettings(ctx context.Context, cfg *SubscriptionConversionSettings) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := s.settingRepo.Set(ctx, SettingKeySubscriptionConversion, string(raw)); err != nil {
		return err
	}
	if s.onUpdate != nil {
		s.onUpdate()
	}
	return nil
}
