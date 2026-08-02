package services

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/pkg/utils"
)

// InstanceSettingsService owns runtime configuration.
//
// The settings are read on nearly every render — every timestamp needs the
// timezone — and written about once a year, so they are cached in memory and
// refreshed on save rather than read back per request.
type InstanceSettingsService struct {
	repo ports.InstanceSettingsRepository

	mu       sync.RWMutex
	cached   domain.InstanceSettings
	isLoaded bool
}

func NewInstanceSettingsService(repo ports.InstanceSettingsRepository) *InstanceSettingsService {
	return &InstanceSettingsService{
		repo: repo,
		// Usable before Load runs, so an early render cannot hit a zero value.
		cached: domain.InstanceSettings{Timezone: domain.DefaultTimezone},
	}
}

// Load primes the cache at boot. A failure is logged and left on defaults
// rather than stopping the server: the instance is still usable in UTC.
func (s *InstanceSettingsService) Load(ctx context.Context) error {
	settings, err := s.repo.Get(ctx)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Could not load instance settings, staying on defaults")
		return err
	}
	s.mu.Lock()
	s.cached = *settings
	s.isLoaded = true
	s.mu.Unlock()
	return nil
}

// Current returns the live settings. Cheap enough to call per render.
func (s *InstanceSettingsService) Current() domain.InstanceSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cached
}

// UpdateTimezone validates and stores the zone, then refreshes the cache so the
// next page renders in it without a restart.
func (s *InstanceSettingsService) UpdateTimezone(ctx context.Context, name string) (int, error) {
	name = strings.TrimSpace(name)
	if !domain.ValidTimezone(name) {
		return http.StatusBadRequest, utils.NewError(
			[]utils.FieldError{utils.CreateFieldError(
				domain.ERR_INVALID_INSTANCE_SETTING_ERR_CODE, domain.ERR_INVALID_INSTANCE_SETTING_ERR,
				"timezone", "That is not a timezone this server recognises")},
			domain.ERR_INVALID_INSTANCE_SETTING_ERR_CODE, errors.New(domain.ERR_INVALID_INSTANCE_SETTING_ERR))
	}

	settings := s.Current()
	settings.Timezone = name
	if err := s.repo.Save(ctx, &settings); err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to save instance settings")
		return http.StatusInternalServerError, utils.NewError(nil,
			domain.ERR_INVALID_INSTANCE_SETTING_ERR_CODE, errors.New("Could not save the setting"))
	}

	s.mu.Lock()
	s.cached = settings
	s.mu.Unlock()

	cslog.L(ctx).WithField("timezone", name).Info("Instance timezone changed")
	return http.StatusOK, nil
}
