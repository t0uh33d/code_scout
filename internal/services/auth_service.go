package services

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/pkg/utils"
	"golang.org/x/crypto/bcrypt"
)

const sessionDuration = 30 * 24 * time.Hour // 30 days

type AuthService struct {
	repo ports.UserRepository
}

func NewAuthService(repo ports.UserRepository) *AuthService {
	return &AuthService{repo: repo}
}

func (s *AuthService) IsFirstRun(ctx context.Context) (bool, error) {
	count, err := s.repo.Count(ctx)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

func (s *AuthService) LoginOrRegister(ctx context.Context, opts *domain.AuthOpts) (string, bool, int, error) {
	log := cslog.L(ctx)

	opts.Username = strings.TrimSpace(opts.Username)

	if opts.Username == "" {
		return "", false, http.StatusBadRequest, utils.NewError(nil, domain.ERR_INVALID_USERNAME_ERR_CODE, errors.New(domain.ERR_INVALID_USERNAME_ERR))
	}
	if len(opts.Password) < 6 {
		return "", false, http.StatusBadRequest, utils.NewError(nil, domain.ERR_INVALID_PASSWORD_ERR_CODE, errors.New(domain.ERR_INVALID_PASSWORD_ERR))
	}

	isFirst, err := s.IsFirstRun(ctx)
	if err != nil {
		log.WithError(err).Error("LoginOrRegister: failed to check first run")
		return "", false, http.StatusInternalServerError, utils.NewError(nil, domain.ERR_INVALID_CREDENTIALS_ERR_CODE, errors.New(domain.ERR_INVALID_CREDENTIALS_ERR))
	}

	if isFirst {
		return s.register(ctx, opts)
	}
	return s.login(ctx, opts)
}

func (s *AuthService) register(ctx context.Context, opts *domain.AuthOpts) (string, bool, int, error) {
	log := cslog.L(ctx)

	if opts.Password != opts.ConfirmPassword {
		return "", false, http.StatusBadRequest, utils.NewError(nil, domain.ERR_PASSWORDS_DO_NOT_MATCH_CODE, errors.New(domain.ERR_PASSWORDS_DO_NOT_MATCH))
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(opts.Password), bcrypt.DefaultCost)
	if err != nil {
		log.WithError(err).Error("register: bcrypt failed")
		return "", false, http.StatusInternalServerError, utils.NewError(nil, domain.ERR_INVALID_PASSWORD_ERR_CODE, errors.New("Failed to process password"))
	}

	user := &domain.User{
		Username:     opts.Username,
		PasswordHash: string(hash),
	}

	if err := s.repo.Create(ctx, user); err != nil {
		log.WithError(err).Error("register: failed to create user")
		return "", false, http.StatusConflict, utils.NewError(nil, domain.ERR_USER_ALREADY_EXISTS_ERR_CODE, errors.New(domain.ERR_USER_ALREADY_EXISTS_ERR))
	}

	token, status, err := s.createSession(ctx, user.ID)
	if err != nil {
		return "", false, status, err
	}

	log.WithField("username", user.Username).Info("New user registered")
	return token, true, http.StatusOK, nil
}

func (s *AuthService) login(ctx context.Context, opts *domain.AuthOpts) (string, bool, int, error) {
	log := cslog.L(ctx)

	user, err := s.repo.GetByUsername(ctx, opts.Username)
	if err != nil {
		return "", false, http.StatusUnauthorized, utils.NewError(nil, domain.ERR_INVALID_CREDENTIALS_ERR_CODE, errors.New(domain.ERR_INVALID_CREDENTIALS_ERR))
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(opts.Password)); err != nil {
		return "", false, http.StatusUnauthorized, utils.NewError(nil, domain.ERR_INVALID_CREDENTIALS_ERR_CODE, errors.New(domain.ERR_INVALID_CREDENTIALS_ERR))
	}

	token, status, err := s.createSession(ctx, user.ID)
	if err != nil {
		return "", false, status, err
	}

	log.WithField("username", user.Username).Info("User logged in")
	return token, false, http.StatusOK, nil
}

func (s *AuthService) createSession(ctx context.Context, userID uuid.UUID) (string, int, error) {
	token := uuid.New().String()
	session := &domain.UserSession{
		UserID:    userID,
		Token:     token,
		ExpiresAt: time.Now().Add(sessionDuration),
	}

	if err := s.repo.CreateSession(ctx, session); err != nil {
		return "", http.StatusInternalServerError, utils.NewError(nil, domain.ERR_INVALID_CREDENTIALS_ERR_CODE, errors.New("Failed to create session"))
	}

	return token, http.StatusOK, nil
}

func (s *AuthService) Logout(ctx context.Context, token string) (int, error) {
	if err := s.repo.DeleteSession(ctx, token); err != nil {
		return http.StatusInternalServerError, utils.NewError(nil, domain.ERR_SESSION_NOT_FOUND_ERR_CODE, errors.New(domain.ERR_SESSION_NOT_FOUND_ERR))
	}
	return http.StatusOK, nil
}

func (s *AuthService) ValidateSession(ctx context.Context, token string) (*domain.User, int, error) {
	session, err := s.repo.GetSessionByToken(ctx, token)
	if err != nil {
		return nil, http.StatusUnauthorized, utils.NewError(nil, domain.ERR_SESSION_NOT_FOUND_ERR_CODE, errors.New(domain.ERR_SESSION_NOT_FOUND_ERR))
	}

	if time.Now().After(session.ExpiresAt) {
		_ = s.repo.DeleteSession(ctx, token)
		return nil, http.StatusUnauthorized, utils.NewError(nil, domain.ERR_SESSION_EXPIRED_ERR_CODE, errors.New(domain.ERR_SESSION_EXPIRED_ERR))
	}

	return &domain.User{ID: session.UserID}, http.StatusOK, nil
}
