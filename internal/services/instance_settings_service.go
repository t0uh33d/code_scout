package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/pkg/utils"
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
		cached: domain.DefaultInstanceSettings(),
	}
}

// Loaded reports whether the settings were actually read from the database.
//
// Load deliberately fails open, which is right for a display timezone and
// wrong for anything destructive: retention runs a delete, and running it
// against defaults because the row was unreadable at boot would throw away
// logs an operator had asked to keep for a year.
func (s *InstanceSettingsService) Loaded() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isLoaded
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

// UpdateRetention stores how long logs live and how long a soft-deleted row
// lingers before it is gone.
//
// Takes the raw form strings rather than ints: "abc" has to produce the same
// inline error as an out-of-range number, and keeping the parse here keeps
// every message for this setting in one file.
func (s *InstanceSettingsService) UpdateRetention(ctx context.Context, keepDays, purgeDays string) (int, error) {
	var fields []utils.FieldError

	keep, err := strconv.Atoi(strings.TrimSpace(keepDays))
	if err != nil || !domain.ValidRetentionDays(keep) {
		fields = append(fields, settingError("retention_days",
			fmt.Sprintf("Enter a whole number of days between %d and %d.",
				domain.MinRetentionDays, domain.MaxRetentionDays)))
	}

	purge, err := strconv.Atoi(strings.TrimSpace(purgeDays))
	if err != nil || !domain.ValidPurgeAfterDays(purge) {
		fields = append(fields, settingError("purge_after_days",
			fmt.Sprintf("Enter a whole number of days between %d and %d.",
				domain.MinPurgeAfterDays, domain.MaxPurgeAfterDays)))
	}

	if len(fields) > 0 {
		return http.StatusBadRequest, utils.NewError(fields,
			domain.ERR_INVALID_INSTANCE_SETTING_ERR_CODE,
			errors.New(domain.ERR_INVALID_INSTANCE_SETTING_ERR))
	}

	settings := s.Current()
	settings.RetentionDays = keep
	settings.PurgeAfterDays = purge
	return s.save(ctx, settings, "retention")
}

// UpdateLimits stores the upload ceiling, in the megabytes a person types
// rather than the bytes the server enforces.
func (s *InstanceSettingsService) UpdateLimits(ctx context.Context, maxUploadMB, dailyCap string) (int, error) {
	var fields []utils.FieldError

	mb, err := strconv.Atoi(strings.TrimSpace(maxUploadMB))
	if err != nil || !domain.ValidMaxUploadMB(mb) {
		fields = append(fields, settingError("max_upload_mb",
			fmt.Sprintf("Enter a size in MB between %d and %d.",
				domain.MinMaxUploadMB, domain.MaxMaxUploadMB)))
	}

	// Empty or "0" both mean uncapped, which is the honest reading of an
	// operator clearing the field.
	capText := strings.TrimSpace(dailyCap)
	var capValue int64
	if capText != "" {
		parsed, err := strconv.ParseInt(capText, 10, 64)
		if err != nil || !domain.ValidDailyLogCap(parsed) {
			fields = append(fields, settingError("daily_log_cap",
				fmt.Sprintf("Enter 0 for no cap, or at least %d logs a day.", domain.MinDailyLogCap)))
		} else {
			capValue = parsed
		}
	}

	if len(fields) > 0 {
		return http.StatusBadRequest, utils.NewError(fields,
			domain.ERR_INVALID_INSTANCE_SETTING_ERR_CODE,
			errors.New(domain.ERR_INVALID_INSTANCE_SETTING_ERR))
	}

	settings := s.Current()
	settings.MaxUploadBytes = int64(mb) << 20
	settings.DailyLogCap = capValue
	return s.save(ctx, settings, "limits")
}

// save is the write half every setter shares: persist, then refresh the cache
// so the next read sees it without a restart.
//
// Every setter starts from Current(), so the whole struct is written back and
// two setters cannot clobber each other's fields. Two super admins saving
// different cards at the same instant still lose one update — accepted for a
// one-row table behind a super-admin gate, and cheaper than a row lock nobody
// else needs.
func (s *InstanceSettingsService) save(ctx context.Context, settings domain.InstanceSettings, what string) (int, error) {
	if err := s.repo.Save(ctx, &settings); err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to save instance settings")
		return http.StatusInternalServerError, utils.NewError(nil,
			domain.ERR_INVALID_INSTANCE_SETTING_ERR_CODE, errors.New("Could not save the setting"))
	}

	s.mu.Lock()
	s.cached = settings
	s.mu.Unlock()

	cslog.L(ctx).WithField("setting", what).Info("Instance settings changed")
	return http.StatusOK, nil
}

func settingError(field, detail string) utils.FieldError {
	return utils.CreateFieldError(
		domain.ERR_INVALID_INSTANCE_SETTING_ERR_CODE, domain.ERR_INVALID_INSTANCE_SETTING_ERR,
		field, detail)
}
