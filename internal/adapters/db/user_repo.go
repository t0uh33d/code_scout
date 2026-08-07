package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// List returns every live account, newest role first so super admins lead.
func (r *UserRepo) List(ctx context.Context) ([]domain.User, error) {
	db := getDB(ctx, r.db)
	var models []UserModel
	err := db.WithContext(ctx).
		Order("CASE role WHEN 'super_admin' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, name ASC").
		Find(&models).Error
	if err != nil {
		cslog.L(ctx).WithError(err).Error("DB: ListUsers failed")
		return nil, err
	}
	users := make([]domain.User, 0, len(models))
	for i := range models {
		users = append(users, *UserModelToDomain(&models[i]))
	}
	return users, nil
}

// CountSuperAdminsForUpdate counts every super admin, locking ALL of their rows
// for the rest of the transaction.
//
// Locking all of them, rather than only the ones excluding some target, is what
// makes the guard safe. Two people removing each other lock different target
// rows and would never block; locking the whole set gives the two transactions
// a shared resource, so the second one waits, re-reads after the first commits,
// and sees the count it is actually deciding against.
func (r *UserRepo) CountSuperAdminsForUpdate(ctx context.Context) (int64, error) {
	db := getDB(ctx, r.db)
	var ids []uuid.UUID
	err := db.WithContext(ctx).
		Model(&UserModel{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("role = ?", string(domain.RoleSuperAdmin)).
		Order("id").
		Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

func (r *UserRepo) UpdateRole(ctx context.Context, userID uuid.UUID, role string) error {
	db := getDB(ctx, r.db)
	res := db.WithContext(ctx).Model(&UserModel{}).Where("id = ?", userID).Update("role", role)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *UserRepo) SetMustChangePassword(ctx context.Context, userID uuid.UUID, must bool) error {
	db := getDB(ctx, r.db)
	return db.WithContext(ctx).Model(&UserModel{}).
		Where("id = ?", userID).
		Update("must_change_password", must).Error
}

// Delete removes an account outright. Sessions and memberships are removed by
// the caller in the same transaction.
//
// Hard, not soft: email carries a unique index that ignores deleted_at, so a
// tombstone would reserve that address forever and the person could never be
// re-added. Nothing references users except sessions and memberships, both of
// which the caller has already removed.
func (r *UserRepo) Delete(ctx context.Context, userID uuid.UUID) error {
	db := getDB(ctx, r.db)
	res := db.WithContext(ctx).Unscoped().Where("id = ?", userID).Delete(&UserModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
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

// The session token is never logged, here or anywhere. It is the whole of the
// `cs_session` cookie, so a line carrying it is a line anyone who can read the
// log can paste into a browser and become that user. It was logged at debug on
// both of these, and debug was on by default.
//
// Nothing is lost by leaving it out: the message says which operation ran, and
// the request id already ties the line to the request.
func (r *UserRepo) GetSessionByToken(ctx context.Context, token string) (*domain.UserSession, error) {
	log := cslog.L(ctx)
	log.Debug("DB: GetSessionByToken")

	db := getDB(ctx, r.db)
	model := &UserSessionModel{}
	err := db.WithContext(ctx).Where("token = ? AND expires_at > ?", token, time.Now()).First(model).Error
	if err != nil {
		// Not found is the ordinary answer for an expired cookie or a signed-out
		// browser, which is most of the traffic to a login page. Logging it at
		// error made routine behaviour look like a fault.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Debug("DB: no live session for that token")
		} else {
			log.WithError(err).Error("DB: GetSessionByToken failed")
		}
		return nil, err
	}
	return UserSessionModelToDomain(model, model.UserID), nil
}

func (r *UserRepo) DeleteSession(ctx context.Context, token string) error {
	log := cslog.L(ctx)
	log.Debug("DB: DeleteSession")

	db := getDB(ctx, r.db)
	return db.WithContext(ctx).Where("token = ?", token).Delete(&UserSessionModel{}).Error
}
