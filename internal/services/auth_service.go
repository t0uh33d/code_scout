package services

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
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

// normalizeEmail validates and canonicalises a login identifier. The PARSED
// address is what gets kept: mail.ParseAddress accepts RFC 5322 name-addr forms
// like "Bob <bob@x.com>", and storing the raw string would create an account
// whose real address can never log in or be reset.
func normalizeEmail(raw string) (string, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	// Lower-cased so the unique index on email is effectively case-insensitive.
	return strings.ToLower(addr.Address), nil
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

	email, err := normalizeEmail(opts.Email)
	if err != nil {
		return "", false, http.StatusBadRequest, utils.NewError(nil, domain.ERR_INVALID_EMAIL_ERR_CODE, errors.New(domain.ERR_INVALID_EMAIL_ERR))
	}
	opts.Email = email
	opts.Name = strings.TrimSpace(opts.Name)
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

	if opts.Name == "" {
		return "", false, http.StatusBadRequest, utils.NewError(nil, domain.ERR_INVALID_NAME_ERR_CODE, errors.New(domain.ERR_INVALID_NAME_ERR))
	}
	if opts.Password != opts.ConfirmPassword {
		return "", false, http.StatusBadRequest, utils.NewError(nil, domain.ERR_PASSWORDS_DO_NOT_MATCH_CODE, errors.New(domain.ERR_PASSWORDS_DO_NOT_MATCH))
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(opts.Password), bcrypt.DefaultCost)
	if err != nil {
		log.WithError(err).Error("register: bcrypt failed")
		return "", false, http.StatusInternalServerError, utils.NewError(nil, domain.ERR_INVALID_PASSWORD_ERR_CODE, errors.New("Failed to process password"))
	}

	// register only runs on a fresh install, so this account is the first one
	// and becomes a super admin. Every later account is created by someone
	// already signed in, through the Members screen.
	user := &domain.User{
		Name:         opts.Name,
		Email:        opts.Email,
		Role:         domain.RoleSuperAdmin,
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

	log.WithField("email", user.Email).Info("New user registered")
	return token, true, http.StatusOK, nil
}

func (s *AuthService) login(ctx context.Context, opts *domain.AuthOpts) (string, bool, int, error) {
	log := cslog.L(ctx)

	user, err := s.repo.GetByEmail(ctx, opts.Email)
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

	log.WithField("email", user.Email).Info("User logged in")
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

// ResetPassword sets a fresh random temporary password for the account and
// signs it out everywhere. The temp password is returned exactly once; only a
// bcrypt hash of it is stored. Used by the reset-password CLI subcommand today
// and by the admin reset flow when Members ships.
func (s *AuthService) ResetPassword(ctx context.Context, email string) (string, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return "", errors.New("invalid email address")
	}

	user, err := s.repo.GetByEmail(ctx, email)
	if err != nil {
		return "", errors.New("no account found for " + email)
	}

	tempPassword := utils.GenerateRandomString(16)
	hash, err := bcrypt.GenerateFromPassword([]byte(tempPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	if err := s.repo.UpdatePasswordHash(ctx, user.ID, string(hash)); err != nil {
		return "", err
	}
	// Invalidate every session AFTER the password change: if this ordering ever
	// fails halfway, the account still has the new password rather than old
	// sessions surviving a "successful" reset.
	if err := s.repo.DeleteSessionsByUserID(ctx, user.ID); err != nil {
		return "", err
	}

	cslog.L(ctx).WithField("email", email).Info("Password reset")
	return tempPassword, nil
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

	// The full record, not just the id: callers need the name and email to
	// render the account menu, and the id alone to scope favourites.
	user, err := s.repo.GetByID(ctx, session.UserID)
	if err != nil {
		return nil, http.StatusUnauthorized, utils.NewError(nil, domain.ERR_SESSION_NOT_FOUND_ERR_CODE, errors.New(domain.ERR_SESSION_NOT_FOUND_ERR))
	}
	return user, http.StatusOK, nil
}
