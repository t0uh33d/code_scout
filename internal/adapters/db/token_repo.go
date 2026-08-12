package db

import (
	"errors"
	"time"

	"context"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TokenRepo struct {
	db *gorm.DB
}

func NewTokenRepo(db *gorm.DB) *TokenRepo {
	return &TokenRepo{db: db}
}

// The token is never logged, and neither is its hash. The plaintext is a
// credential outright; the hash is the lookup key for one, and a log line
// carrying it would say which token was used where, which is more than a
// reader needs. The message names the operation and the request id ties it to
// the request — same discipline as GetSessionByToken in user_repo.go.

func (r *TokenRepo) Create(ctx context.Context, token *domain.PersonalAccessToken) error {
	log := cslog.L(ctx)
	log.WithField("user_id", token.UserID).Debug("DB: CreatePersonalToken")

	db := getDB(ctx, r.db)
	model := TokenDomainToModel(token)
	if err := db.WithContext(ctx).Create(model).Error; err != nil {
		log.WithError(err).Error("DB: CreatePersonalToken failed")
		return err
	}
	token.ID = model.ID
	token.CreatedAt = model.CreatedAt
	token.UpdatedAt = model.UpdatedAt
	return nil
}

func (r *TokenRepo) GetByHash(ctx context.Context, hash string) (*domain.PersonalAccessToken, error) {
	log := cslog.L(ctx)
	log.Debug("DB: GetPersonalTokenByHash")

	db := getDB(ctx, r.db)
	model := &PersonalAccessTokenModel{}
	err := db.WithContext(ctx).Where("token_hash = ?", hash).First(model).Error
	if err != nil {
		// A miss is ordinary traffic: a revoked token still configured in
		// somebody's editor. Not an error.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Debug("DB: no token for that hash")
			return nil, domain.ErrNotFound
		}
		log.WithError(err).Error("DB: GetPersonalTokenByHash failed")
		return nil, err
	}
	return TokenModelToDomain(model), nil
}

func (r *TokenRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.PersonalAccessToken, error) {
	db := getDB(ctx, r.db)
	var models []PersonalAccessTokenModel
	err := db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&models).Error
	if err != nil {
		cslog.L(ctx).WithError(err).Error("DB: ListPersonalTokens failed")
		return nil, err
	}
	tokens := make([]domain.PersonalAccessToken, 0, len(models))
	for i := range models {
		tokens = append(tokens, *TokenModelToDomain(&models[i]))
	}
	return tokens, nil
}

// Count is live tokens only; the default scope excludes revoked ones, which is
// what the per-user cap should count.
func (r *TokenRepo) Count(ctx context.Context, userID uuid.UUID) (int64, error) {
	db := getDB(ctx, r.db)
	var count int64
	err := db.WithContext(ctx).Model(&PersonalAccessTokenModel{}).
		Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// Revoke soft-deletes one token, scoped to its owner so a handler cannot be
// talked into revoking somebody else's by id. GORM's default scope excludes
// soft-deleted rows from GetByHash, so revocation takes effect on the very
// next request.
func (r *TokenRepo) Revoke(ctx context.Context, userID, tokenID uuid.UUID) error {
	log := cslog.L(ctx)
	log.WithField("token_id", tokenID).Debug("DB: RevokePersonalToken")

	db := getDB(ctx, r.db)
	res := db.WithContext(ctx).
		Where("user_id = ? AND id = ?", userID, tokenID).
		Delete(&PersonalAccessTokenModel{})
	if res.Error != nil {
		log.WithError(res.Error).Error("DB: RevokePersonalToken failed")
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// TouchLastUsed stamps the token, but only when the stored stamp is older than
// since. The guard lives in the WHERE clause so two concurrent requests
// produce one write, not two.
func (r *TokenRepo) TouchLastUsed(ctx context.Context, tokenID uuid.UUID, at time.Time, since time.Time) error {
	db := getDB(ctx, r.db)
	return db.WithContext(ctx).Model(&PersonalAccessTokenModel{}).
		Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", tokenID, since).
		Update("last_used_at", at).Error
}

// DeleteByUser removes every token, revoked ones included, when the account
// goes. Unscoped for the same reason DeleteSessionsByUserID is: credentials
// are not history worth keeping.
func (r *TokenRepo) DeleteByUser(ctx context.Context, userID uuid.UUID) error {
	log := cslog.L(ctx)
	log.WithField("user_id", userID).Debug("DB: DeletePersonalTokensByUser")

	db := getDB(ctx, r.db)
	return db.WithContext(ctx).Unscoped().
		Where("user_id = ?", userID).
		Delete(&PersonalAccessTokenModel{}).Error
}
