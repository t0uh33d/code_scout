package services

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/pkg/utils"
	"golang.org/x/crypto/bcrypt"
)

// tempPasswordLength is long enough to be safe and short enough to read aloud
// or retype from a chat message, which is how it actually gets delivered.
const tempPasswordLength = 14

// MemberService owns accounts and project membership. Every method takes the
// acting user, because almost every rule here depends on who is asking.
type MemberService struct {
	users   ports.UserRepository
	members ports.MemberRepository
	tokens  ports.TokenRepository
	txMgr   ports.TransactionManager
}

func NewMemberService(users ports.UserRepository, members ports.MemberRepository, tokens ports.TokenRepository, txMgr ports.TransactionManager) *MemberService {
	return &MemberService{users: users, members: members, tokens: tokens, txMgr: txMgr}
}

func forbidden(msg string) error {
	return utils.NewError(nil, domain.UNAUTHORIZED, errors.New(msg))
}

// ListMembers returns every account on the instance. Accounts are instance
// scoped, so any signed-in user may see who exists; what they may *do* to those
// accounts is decided per action.
func (s *MemberService) ListMembers(ctx context.Context) ([]domain.User, int, error) {
	users, err := s.users.List(ctx)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list members")
		return nil, http.StatusInternalServerError, forbidden("Could not load members")
	}
	return users, http.StatusOK, nil
}

// CreateMember adds an account and returns the one-time temporary password.
// No mail is ever sent, so the caller has to hand it over directly.
func (s *MemberService) CreateMember(ctx context.Context, actor *domain.User, opts *domain.CreateMemberOpts) (*domain.User, string, int, error) {
	log := cslog.L(ctx)

	if actor == nil || !actor.CanCreateProjects() {
		return nil, "", http.StatusForbidden, forbidden("You cannot add members")
	}
	// You may only create accounts below your own rank, so an admin cannot
	// mint another admin and quietly widen the set of people who can add
	// accounts.
	if !opts.Role.Valid() {
		return nil, "", http.StatusBadRequest, forbidden("Unknown role")
	}
	if !actor.Role.Outranks(opts.Role) {
		return nil, "", http.StatusForbidden, forbidden("You cannot create an account with that role")
	}

	email, err := normalizeEmail(opts.Email)
	if err != nil {
		return nil, "", http.StatusBadRequest, utils.NewError(
			[]utils.FieldError{utils.CreateFieldError(domain.ERR_INVALID_EMAIL_ERR_CODE, domain.ERR_INVALID_EMAIL_ERR, "email", "Enter a valid email address")},
			domain.ERR_INVALID_EMAIL_ERR_CODE, errors.New(domain.ERR_INVALID_EMAIL_ERR))
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return nil, "", http.StatusBadRequest, utils.NewError(
			[]utils.FieldError{utils.CreateFieldError(domain.ERR_INVALID_NAME_ERR_CODE, domain.ERR_INVALID_NAME_ERR, "name", "Name cannot be empty")},
			domain.ERR_INVALID_NAME_ERR_CODE, errors.New(domain.ERR_INVALID_NAME_ERR))
	}

	if existing, err := s.users.GetByEmail(ctx, email); err == nil && existing != nil {
		return nil, "", http.StatusConflict, utils.NewError(
			[]utils.FieldError{utils.CreateFieldError(domain.ERR_USER_ALREADY_EXISTS_ERR_CODE, domain.ERR_USER_ALREADY_EXISTS_ERR, "email", "An account with that email already exists")},
			domain.ERR_USER_ALREADY_EXISTS_ERR_CODE, errors.New(domain.ERR_USER_ALREADY_EXISTS_ERR))
	}

	tempPassword := utils.GenerateRandomString(tempPasswordLength)
	hash, err := bcrypt.GenerateFromPassword([]byte(tempPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", http.StatusInternalServerError, forbidden("Could not create the account")
	}

	user := &domain.User{
		Name:               name,
		Email:              email,
		Role:               opts.Role,
		PasswordHash:       string(hash),
		MustChangePassword: true,
	}

	// Project ids arrive from a form, so they are attacker-controlled. You may
	// only hand out access to projects you can manage yourself, or an admin
	// could grant a new account access to a project they cannot even see and
	// then sign in as them.
	granted := make([]domain.ProjectAssignment, 0, len(opts.Projects))
	for _, p := range opts.Projects {
		if p.ProjectID == uuid.Nil || !p.Level.Valid() {
			continue
		}
		access, err := s.ResolveAccess(ctx, actor, p.ProjectID)
		if err != nil {
			return nil, "", http.StatusInternalServerError, forbidden("Could not check project access")
		}
		if !access.CanManage {
			return nil, "", http.StatusForbidden, forbidden("You cannot grant access to a project you do not manage")
		}
		granted = append(granted, p)
	}

	err = s.txMgr.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.users.Create(txCtx, user); err != nil {
			return err
		}
		// Admins see nothing by role alone, so project assignments matter for
		// them exactly as much as for members. The user id comes from the row
		// just created, never from the form.
		for _, p := range granted {
			if err := s.members.SetMembership(txCtx, user.ID, p.ProjectID, p.Level); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.WithError(err).Error("Failed to create member")
		return nil, "", http.StatusInternalServerError, forbidden("Could not create the account")
	}

	log.WithField("email", email).WithField("role", opts.Role).Info("Member created")
	return user, tempPassword, http.StatusOK, nil
}

// ResetMemberPassword issues a new temporary password and signs the account out
// everywhere. Account operations never flow from project membership: holding
// maintainer on a project does not let you reset anyone's password.
func (s *MemberService) ResetMemberPassword(ctx context.Context, actor *domain.User, targetID uuid.UUID) (string, int, error) {
	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return "", http.StatusNotFound, forbidden("Account not found")
	}
	if !domain.CanManageAccount(actor, target) {
		return "", http.StatusForbidden, forbidden("You cannot reset that password")
	}

	password, err := s.resetTo(ctx, target.ID)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to reset member password")
		return "", http.StatusInternalServerError, forbidden("Could not reset the password")
	}
	cslog.L(ctx).WithField("target", target.Email).WithField("by", actor.Email).Info("Password reset by an operator")
	return password, http.StatusOK, nil
}

func (s *MemberService) resetTo(ctx context.Context, userID uuid.UUID) (string, error) {
	password := utils.GenerateRandomString(tempPasswordLength)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	err = s.txMgr.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.users.UpdatePasswordHash(txCtx, userID, string(hash)); err != nil {
			return err
		}
		if err := s.users.SetMustChangePassword(txCtx, userID, true); err != nil {
			return err
		}
		// After the password, so a failure leaves the account on the new
		// password rather than "reset succeeded" with old sessions alive.
		return s.users.DeleteSessionsByUserID(txCtx, userID)
	})
	return password, err
}

// ChangeRole promotes or demotes an account.
func (s *MemberService) ChangeRole(ctx context.Context, actor *domain.User, targetID uuid.UUID, role domain.Role) (int, error) {
	if !role.Valid() {
		return http.StatusBadRequest, forbidden("Unknown role")
	}
	if actor == nil || actor.Role != domain.RoleSuperAdmin {
		// Only a super admin changes roles. An admin promoting someone to admin
		// would be a way to widen the set of account creators sideways.
		return http.StatusForbidden, forbidden("Only a super admin can change roles")
	}

	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return http.StatusNotFound, forbidden("Account not found")
	}
	if target.Role == role {
		return http.StatusOK, nil
	}

	// Demoting a super admin, including yourself, must not remove the last one.
	if target.Role == domain.RoleSuperAdmin && role != domain.RoleSuperAdmin {
		if err := s.guardLastSuperAdmin(ctx, targetID, func(txCtx context.Context) error {
			return s.users.UpdateRole(txCtx, targetID, string(role))
		}); err != nil {
			return statusForGuard(err)
		}
		return http.StatusOK, nil
	}

	if err := s.users.UpdateRole(ctx, targetID, string(role)); err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to change role")
		return http.StatusInternalServerError, forbidden("Could not change the role")
	}
	return http.StatusOK, nil
}

// DeleteMember removes an account entirely.
func (s *MemberService) DeleteMember(ctx context.Context, actor *domain.User, targetID uuid.UUID) (int, error) {
	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return http.StatusNotFound, forbidden("Account not found")
	}

	// Super admins may remove each other freely; the only refusal is the last
	// one. Everyone else may only remove strictly below themselves.
	if target.Role == domain.RoleSuperAdmin {
		if actor == nil || actor.Role != domain.RoleSuperAdmin {
			return http.StatusForbidden, forbidden("Only a super admin can remove a super admin")
		}
		if err := s.guardLastSuperAdmin(ctx, targetID, func(txCtx context.Context) error {
			if err := s.members.RemoveAllForUser(txCtx, targetID); err != nil {
				return err
			}
			if err := s.users.DeleteSessionsByUserID(txCtx, targetID); err != nil {
				return err
			}
			// Tokens go with the account for the same reason sessions do: a
			// credential for a user who no longer exists is not history.
			if err := s.tokens.DeleteByUser(txCtx, targetID); err != nil {
				return err
			}
			return s.users.Delete(txCtx, targetID)
		}); err != nil {
			return statusForGuard(err)
		}
		return http.StatusOK, nil
	}

	if !domain.CanManageAccount(actor, target) {
		return http.StatusForbidden, forbidden("You cannot remove that account")
	}

	err = s.txMgr.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.members.RemoveAllForUser(txCtx, targetID); err != nil {
			return err
		}
		if err := s.users.DeleteSessionsByUserID(txCtx, targetID); err != nil {
			return err
		}
		if err := s.tokens.DeleteByUser(txCtx, targetID); err != nil {
			return err
		}
		return s.users.Delete(txCtx, targetID)
	})
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to delete member")
		return http.StatusInternalServerError, forbidden("Could not remove the account")
	}
	return http.StatusOK, nil
}

// errLastSuperAdmin is returned when an operation would leave the instance with
// nobody able to administer it.
var errLastSuperAdmin = errors.New("the last super admin cannot be removed or demoted")

// guardLastSuperAdmin runs fn only if another super admin would survive it.
//
// The count and the write happen in ONE transaction with the rows locked.
// Without the lock, two super admins removing each other at the same moment
// would both see "there are two of us", both commit, and leave zero.
func (s *MemberService) guardLastSuperAdmin(ctx context.Context, targetID uuid.UUID, fn func(context.Context) error) error {
	return s.txMgr.WithTransaction(ctx, func(txCtx context.Context) error {
		// Counts and locks ALL super admins, target included. The target is
		// still a super admin at this point, so "more than one" is what makes
		// removing this one safe.
		total, err := s.users.CountSuperAdminsForUpdate(txCtx)
		if err != nil {
			return err
		}
		if total <= 1 {
			return errLastSuperAdmin
		}
		return fn(txCtx)
	})
}

func statusForGuard(err error) (int, error) {
	if errors.Is(err, errLastSuperAdmin) {
		return http.StatusConflict, forbidden("This is the last super admin. Promote someone else first.")
	}
	return http.StatusInternalServerError, forbidden("Could not complete that change")
}

// SetProjectAccess adds or updates someone's level on a project.
func (s *MemberService) SetProjectAccess(ctx context.Context, actor *domain.User, projectID, targetID uuid.UUID, level domain.ProjectLevel) (int, error) {
	if !level.Valid() {
		return http.StatusBadRequest, forbidden("Unknown access level")
	}
	if code, err := s.requireManage(ctx, actor, projectID); err != nil {
		return code, err
	}

	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return http.StatusNotFound, forbidden("Account not found")
	}
	// A super admin already sees everything; a row would imply their access is
	// revocable, which it is not.
	if target.Role == domain.RoleSuperAdmin {
		return http.StatusBadRequest, forbidden("Super admins already have access to every project")
	}

	if err := s.members.SetMembership(ctx, targetID, projectID, level); err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to set project access")
		return http.StatusInternalServerError, forbidden("Could not update access")
	}
	return http.StatusOK, nil
}

// RemoveProjectAccess revokes someone's membership of a project.
func (s *MemberService) RemoveProjectAccess(ctx context.Context, actor *domain.User, projectID, targetID uuid.UUID) (int, error) {
	if code, err := s.requireManage(ctx, actor, projectID); err != nil {
		return code, err
	}
	// Removing your own maintainer row would lock you out of a project you can
	// still see in the list, which reads as a bug rather than an action.
	if actor.ID == targetID {
		return http.StatusBadRequest, forbidden("You cannot remove your own access")
	}
	if err := s.members.RemoveMembership(ctx, targetID, projectID); err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to remove project access")
		return http.StatusInternalServerError, forbidden("Could not update access")
	}
	return http.StatusOK, nil
}

// ListProjectMembers returns who is on a project, for the Access panel.
func (s *MemberService) ListProjectMembers(ctx context.Context, actor *domain.User, projectID uuid.UUID) ([]domain.ProjectMember, int, error) {
	access, err := s.ResolveAccess(ctx, actor, projectID)
	if err != nil || !access.CanRead {
		return nil, http.StatusNotFound, forbidden("Project not found")
	}
	members, err := s.members.ListProjectMembers(ctx, projectID)
	if err != nil {
		return nil, http.StatusInternalServerError, forbidden("Could not load access")
	}
	return members, http.StatusOK, nil
}

// GetMember loads one account for display.
func (s *MemberService) GetMember(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return s.users.GetByID(ctx, id)
}

// ProjectIDsFor lists the projects an account has been added to.
func (s *MemberService) ProjectIDsFor(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	return s.members.ListUserProjectIDs(ctx, userID)
}

// ListAssignableAccounts returns accounts that could be added to a project:
// everyone who is not already on it and is not a super admin, since a super
// admin's access is not a membership row and cannot be revoked.
func (s *MemberService) ListAssignableAccounts(ctx context.Context, projectID uuid.UUID) ([]domain.User, error) {
	all, err := s.users.List(ctx)
	if err != nil {
		return nil, err
	}
	existing, err := s.members.ListProjectMembers(ctx, projectID)
	if err != nil {
		return nil, err
	}
	onProject := make(map[uuid.UUID]bool, len(existing))
	for _, m := range existing {
		onProject[m.UserID] = true
	}

	out := make([]domain.User, 0, len(all))
	for _, u := range all {
		if u.Role == domain.RoleSuperAdmin || onProject[u.ID] {
			continue
		}
		out = append(out, u)
	}
	return out, nil
}

// ListCandidatesForNewProject returns accounts the wizard's Access step can
// offer. There is no project yet, so the filter is by account alone: super
// admins already see everything, and the creator becomes maintainer anyway.
func (s *MemberService) ListCandidatesForNewProject(ctx context.Context, actor *domain.User) ([]domain.User, error) {
	all, err := s.users.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.User, 0, len(all))
	for _, u := range all {
		if u.Role == domain.RoleSuperAdmin || (actor != nil && u.ID == actor.ID) {
			continue
		}
		out = append(out, u)
	}
	return out, nil
}

// ResolveAccess answers what the actor may do in a project. Handlers use this
// rather than checking roles themselves.
func (s *MemberService) ResolveAccess(ctx context.Context, actor *domain.User, projectID uuid.UUID) (domain.ProjectAccess, error) {
	if actor == nil {
		return domain.ProjectAccess{}, nil
	}
	if actor.Role == domain.RoleSuperAdmin {
		return domain.ResolveProjectAccess(actor, nil), nil
	}
	membership, err := s.members.GetMembership(ctx, actor.ID, projectID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ProjectAccess{}, nil
		}
		return domain.ProjectAccess{}, err
	}
	return domain.ResolveProjectAccess(actor, membership), nil
}

func (s *MemberService) requireManage(ctx context.Context, actor *domain.User, projectID uuid.UUID) (int, error) {
	access, err := s.ResolveAccess(ctx, actor, projectID)
	if err != nil {
		return http.StatusInternalServerError, forbidden("Could not check access")
	}
	if !access.CanRead {
		// Not "forbidden": someone who cannot see a project should not learn it
		// exists from the error.
		return http.StatusNotFound, forbidden("Project not found")
	}
	if !access.CanManage {
		return http.StatusForbidden, forbidden("You cannot manage access for this project")
	}
	return http.StatusOK, nil
}
