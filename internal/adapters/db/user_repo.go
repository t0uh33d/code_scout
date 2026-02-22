package db

import (
	"context"
	"time"

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

func (r *UserRepo) Count(ctx context.Context, tx *gorm.DB) (int64, error) {
	log := cslog.L(ctx)
	log.Debug("DB: CountUsers")

	var count int64
	err := tx.WithContext(ctx).Model(&UserModel{}).Count(&count).Error
	if err != nil {
		log.WithError(err).Error("DB: CountUsers failed")
		return 0, err
	}
	return count, nil
}

func (r *UserRepo) GetByUsername(ctx context.Context, tx *gorm.DB, username string) (*domain.User, error) {
	log := cslog.L(ctx)
	log.WithField("username", username).Debug("DB: GetUserByUsername")

	model := &UserModel{}
	err := tx.WithContext(ctx).Where("username = ?", username).First(model).Error
	if err != nil {
		log.WithError(err).Error("DB: GetUserByUsername failed")
		return nil, err
	}
	return UserModelToDomain(model), nil
}

func (r *UserRepo) Create(ctx context.Context, tx *gorm.DB, user *domain.User) error {
	log := cslog.L(ctx)
	log.WithField("username", user.Username).Debug("DB: CreateUser")

	model := UserDomainToModel(user)
	if err := tx.WithContext(ctx).Create(model).Error; err != nil {
		log.WithError(err).Error("DB: CreateUser failed")
		return err
	}
	user.ID = model.ID
	user.CreatedAt = model.CreatedAt
	user.UpdatedAt = model.UpdatedAt
	return nil
}

func (r *UserRepo) CreateSession(ctx context.Context, tx *gorm.DB, session *domain.UserSession) error {
	log := cslog.L(ctx)
	log.Debug("DB: CreateUserSession")

	model := UserSessionDomainToModel(session)
	if err := tx.WithContext(ctx).Create(model).Error; err != nil {
		log.WithError(err).Error("DB: CreateUserSession failed")
		return err
	}
	session.ID = model.ID
	return nil
}

func (r *UserRepo) GetSessionByToken(ctx context.Context, tx *gorm.DB, token string) (*domain.UserSession, error) {
	log := cslog.L(ctx)
	log.WithField("token", token).Debug("DB: GetSessionByToken")

	model := &UserSessionModel{}
	err := tx.WithContext(ctx).Where("token = ? AND expires_at > ?", token, time.Now()).First(model).Error
	if err != nil {
		log.WithError(err).Error("DB: GetSessionByToken failed")
		return nil, err
	}
	return UserSessionModelToDomain(model, model.UserID), nil
}

func (r *UserRepo) DeleteSession(ctx context.Context, tx *gorm.DB, token string) error {
	log := cslog.L(ctx)
	log.WithField("token", token).Debug("DB: DeleteSession")

	return tx.WithContext(ctx).Where("token = ?", token).Delete(&UserSessionModel{}).Error
}
