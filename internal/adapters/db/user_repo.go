package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"gorm.io/gorm"
)

type UserRepo struct {
	db *gorm.DB
}

func NewUserRepo(db *gorm.DB) *UserRepo {
	return &UserRepo{db: db}
}

func (r *UserRepo) Count(ctx context.Context) (int64, error) {
	log := cslog.L(ctx)
	log.Debug("DB: CountUsers")

	db := getDB(ctx, r.db)
	var count int64
	err := db.WithContext(ctx).Model(&UserModel{}).Count(&count).Error
	if err != nil {
		log.WithError(err).Error("DB: CountUsers failed")
		return 0, err
	}
	return count, nil
}

func (r *UserRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	db := getDB(ctx, r.db)
	model := &UserModel{}
	if err := db.WithContext(ctx).Where("id = ?", id).First(model).Error; err != nil {
		cslog.L(ctx).WithError(err).Error("DB: GetUserByID failed")
		return nil, err
	}
	return UserModelToDomain(model), nil
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	log := cslog.L(ctx)
	log.WithField("email", email).Debug("DB: GetUserByEmail")

	db := getDB(ctx, r.db)
	model := &UserModel{}
	err := db.WithContext(ctx).Where("email = ?", email).First(model).Error
	if err != nil {
		log.WithError(err).Error("DB: GetUserByEmail failed")
		return nil, err
	}
	return UserModelToDomain(model), nil
}

func (r *UserRepo) Create(ctx context.Context, user *domain.User) error {
	log := cslog.L(ctx)
	log.WithField("email", user.Email).Debug("DB: CreateUser")

	db := getDB(ctx, r.db)
	model := UserDomainToModel(user)
	if err := db.WithContext(ctx).Create(model).Error; err != nil {
		log.WithError(err).Error("DB: CreateUser failed")
		return err
	}
	user.ID = model.ID
	user.CreatedAt = model.CreatedAt
	user.UpdatedAt = model.UpdatedAt
	return nil
}

func (r *UserRepo) UpdatePasswordHash(ctx context.Context, userID uuid.UUID, hash string) error {
	log := cslog.L(ctx)
	log.WithField("user_id", userID).Debug("DB: UpdatePasswordHash")

	db := getDB(ctx, r.db)
	result := db.WithContext(ctx).Model(&UserModel{}).
		Where("id = ?", userID).
		Update("password_hash", hash)
	if result.Error != nil {
		log.WithError(result.Error).Error("DB: UpdatePasswordHash failed")
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteSessionsByUserID signs the user out everywhere. Called after a password
// reset so a stolen session does not survive the reset.
func (r *UserRepo) DeleteSessionsByUserID(ctx context.Context, userID uuid.UUID) error {
	log := cslog.L(ctx)
	log.WithField("user_id", userID).Debug("DB: DeleteSessionsByUserID")

	db := getDB(ctx, r.db)
	// Unscoped: session rows are ephemeral credentials, not history worth
	// keeping, and a soft-deleted one would still block nothing but confuse
	// debugging.
	return db.WithContext(ctx).Unscoped().
		Where("user_id = ?", userID).
		Delete(&UserSessionModel{}).Error
}

func (r *UserRepo) CreateSession(ctx context.Context, session *domain.UserSession) error {
	log := cslog.L(ctx)
	log.Debug("DB: CreateUserSession")

	db := getDB(ctx, r.db)
	model := UserSessionDomainToModel(session)
	if err := db.WithContext(ctx).Create(model).Error; err != nil {
		log.WithError(err).Error("DB: CreateUserSession failed")
		return err
	}
	session.ID = model.ID
	return nil
}

func (r *UserRepo) GetSessionByToken(ctx context.Context, token string) (*domain.UserSession, error) {
	log := cslog.L(ctx)
	log.WithField("token", token).Debug("DB: GetSessionByToken")

	db := getDB(ctx, r.db)
	model := &UserSessionModel{}
	err := db.WithContext(ctx).Where("token = ? AND expires_at > ?", token, time.Now()).First(model).Error
	if err != nil {
		log.WithError(err).Error("DB: GetSessionByToken failed")
		return nil, err
	}
	return UserSessionModelToDomain(model, model.UserID), nil
}

func (r *UserRepo) DeleteSession(ctx context.Context, token string) error {
	log := cslog.L(ctx)
	log.WithField("token", token).Debug("DB: DeleteSession")

	db := getDB(ctx, r.db)
	return db.WithContext(ctx).Where("token = ?", token).Delete(&UserSessionModel{}).Error
}
