package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/pkg/utils"
	"github.com/google/uuid"
)

// maxTokensPerUser caps how many live tokens one account can hold. Nobody
// needs more than a handful; a runaway script minting thousands should hit a
// wall, not a table scan.
const maxTokensPerUser = 20

// touchInterval is how stale last_used_at may be before authenticating writes
// a fresh stamp. The column answers "is anything still using this?", which
// does not need per-request precision, and one UPDATE per request would put a
// write on the hot path of every tool call.
const touchInterval = 5 * time.Minute

// TokenService mints, lists, revokes and checks personal access tokens.
type TokenService struct {
	tokens ports.TokenRepository
	users  ports.UserRepository
}

func NewTokenService(tokens ports.TokenRepository, users ports.UserRepository) *TokenService {
	return &TokenService{tokens: tokens, users: users}
}

// Create mints a token for the user and returns the plaintext exactly once.
// Everything stored is the hash and the display suffix.
func (s *TokenService) Create(ctx context.Context, userID uuid.UUID, name string, expiresIn *time.Duration) (string, *domain.PersonalAccessToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, utils.NewError(nil, domain.ERR_TOKEN_NAME_REQUIRED_ERR_CODE, errors.New(domain.ERR_TOKEN_NAME_REQUIRED_ERR))
	}

	count, err := s.tokens.Count(ctx, userID)
	if err != nil {
		return "", nil, err
	}
	if count >= maxTokensPerUser {
		return "", nil, utils.NewError(nil, domain.ERR_TOKEN_LIMIT_ERR_CODE, errors.New(domain.ERR_TOKEN_LIMIT_ERR))
	}

	plaintext, hash, err := domain.MintPersonalToken()
	if err != nil {
		return "", nil, err
	}
	token := &domain.PersonalAccessToken{
		UserID:    userID,
		Name:      name,
		TokenHash: hash,
		Suffix:    domain.TokenSuffix(plaintext),
	}
	if expiresIn != nil {
		at := time.Now().Add(*expiresIn)
		token.ExpiresAt = &at
	}
	if err := s.tokens.Create(ctx, token); err != nil {
		return "", nil, err
	}
	return plaintext, token, nil
}

func (s *TokenService) ListForUser(ctx context.Context, userID uuid.UUID) ([]domain.PersonalAccessToken, error) {
	return s.tokens.ListByUser(ctx, userID)
}

func (s *TokenService) Revoke(ctx context.Context, userID, tokenID uuid.UUID) error {
	return s.tokens.Revoke(ctx, userID, tokenID)
}

// Authenticate resolves a plaintext token to its user. The plaintext is
// hashed in the first statement and never logged, stored or passed on.
//
// Unknown, revoked and deleted-user tokens all answer with the same error:
// telling them apart would let whoever holds a revoked token probe what
// happened to it.
func (s *TokenService) Authenticate(ctx context.Context, plaintext string) (*domain.User, error) {
	hash := domain.HashPersonalToken(plaintext)

	invalid := func() error {
		return utils.NewError(nil, domain.ERR_TOKEN_INVALID_ERR_CODE, errors.New(domain.ERR_TOKEN_INVALID_ERR))
	}

	// The prefix check costs nothing and keeps random bearer strings from
	// reaching the database at all.
	if !strings.HasPrefix(plaintext, domain.PersonalTokenPrefix) {
		return nil, invalid()
	}

	token, err := s.tokens.GetByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, invalid()
		}
		return nil, err
	}

	now := time.Now()
	if token.Expired(now) {
		// The caller already holds the token, so naming its expiry leaks
		// nothing and tells them exactly what to fix.
		return nil, utils.NewError(nil, domain.ERR_TOKEN_EXPIRED_ERR_CODE, errors.New(domain.ERR_TOKEN_EXPIRED_ERR))
	}

	user, err := s.users.GetByID(ctx, token.UserID)
	if err != nil {
		// A deleted account reads exactly like a bad token, which is what a
		// removed teammate's leftover token should look like.
		return nil, invalid()
	}

	// The session middleware forces this account to the change-password
	// screen. A token that kept working would make a temporary-password
	// reset weaker than a login.
	if user.MustChangePassword {
		return nil, utils.NewError(nil, domain.ERR_TOKEN_PASSWORD_CHANGE_ERR_CODE, errors.New(domain.ERR_TOKEN_PASSWORD_CHANGE_ERR))
	}

	// Stamp last_used_at, at most once per touchInterval. A failed stamp
	// never fails the request — it is bookkeeping, not auth.
	if token.LastUsedAt == nil || now.Sub(*token.LastUsedAt) >= touchInterval {
		if err := s.tokens.TouchLastUsed(ctx, token.ID, now, now.Add(-touchInterval)); err != nil {
			cslog.L(ctx).WithError(err).Debug("token last_used_at stamp failed")
		}
	}

	return user, nil
}
