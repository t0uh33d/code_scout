package handlers

import (
	"errors"
	"net/http"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/pkg/utils"
	"github.com/getcodescout/code_scout/server/middleware"
	"github.com/getcodescout/code_scout/view"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type MemberHandler struct {
	memberSvc  *services.MemberService
	projectSvc ports.ProjectManager
}

func NewMemberHandler(memberSvc *services.MemberService, projectSvc ports.ProjectManager) *MemberHandler {
	return &MemberHandler{memberSvc: memberSvc, projectSvc: projectSvc}
}

// membersData assembles the screen, resolving per-row permissions once here
// rather than letting the template decide who may do what.
// membersData assembles the screen, resolving per-row permissions once here
// rather than letting the template decide who may do what.
//
// A function rather than a method: the instance settings screen renders the
// same pane under its Members tab, and two copies of this would drift.
func membersData(r *http.Request, memberSvc *services.MemberService, projectSvc ports.ProjectManager) view.MembersData {
	ctx := r.Context()
	actor := middleware.UserFrom(ctx)

	users, _, err := memberSvc.ListMembers(ctx)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list members")
	}

	superAdmins := 0
	for _, u := range users {
		if u.Role == domain.RoleSuperAdmin {
			superAdmins++
		}
	}

	// Only count projects the VIEWER can see. Otherwise the column tells an
	// admin how many projects exist that they have no access to, which is the
	// existence disclosure the 404s elsewhere are there to prevent.
	visible := map[uuid.UUID]bool{}
	for _, p := range assignableProjects(r, projectSvc) {
		visible[p.ID] = true
	}

	rows := make([]view.MemberRow, 0, len(users))
	for i := range users {
		u := users[i]
		allIDs, _ := memberSvc.ProjectIDsFor(ctx, u.ID)
		projectIDs := make([]uuid.UUID, 0, len(allIDs))
		for _, id := range allIDs {
			if visible[id] {
				projectIDs = append(projectIDs, id)
			}
		}

		canManage := domain.CanManageAccount(actor, &u)
		// Super admins may remove each other, which CanManageAccount refuses
		// because neither outranks the other. The last one is still protected,
		// and the service enforces that regardless of what the UI offers.
		if actor != nil && actor.Role == domain.RoleSuperAdmin && u.Role == domain.RoleSuperAdmin && actor.ID != u.ID {
			canManage = true
		}

		rows = append(rows, view.MemberRow{
			User:         u,
			ProjectCount: len(projectIDs),
			CanReset:     domain.CanManageAccount(actor, &u),
			CanRemove:    canManage && !(u.Role == domain.RoleSuperAdmin && superAdmins <= 1),
			// Only a super admin changes roles, and never their own.
			CanChangeRole: actor != nil && actor.Role == domain.RoleSuperAdmin && actor.ID != u.ID,
		})
	}

	return view.MembersData{
		User:     actor,
		Members:  rows,
		CanAdd:   actor != nil && actor.CanCreateProjects(),
		Projects: assignableProjects(r, projectSvc),
	}
}

// assignableProjects is what the actor may hand out, which is only what they
// can see. An admin cannot grant access to a project they are not on.
func assignableProjects(r *http.Request, projectSvc ports.ProjectManager) []domain.ProjectListItem {
	ctx := r.Context()
	opts := domain.ProjectListOpts{Page: 1, PageSize: 200}
	opts.ScopeToUser(middleware.UserFrom(ctx))

	result, _, err := projectSvc.ListProjects(ctx, opts)
	if err != nil || result == nil {
		return nil
	}
	return result.Items
}

// Members handles GET /members. Members moved under instance settings, so this
// only forwards anything still pointing at the old address.
func (h *MemberHandler) Members(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/settings?tab=members", http.StatusFound)
}

// NewMember renders the add-member form into the sheet.
func (h *MemberHandler) NewMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	actor := middleware.UserFrom(ctx)
	if actor == nil || !actor.CanCreateProjects() {
		http.Error(w, "You cannot add members", http.StatusForbidden)
		return
	}
	view.AddMemberForm(view.AddMemberFormData{
		Role:            domain.RoleMember,
		Projects:        assignableProjects(r, h.projectSvc),
		CanCreateAdmins: actor.Role == domain.RoleSuperAdmin,
	}).Render(ctx, w)
}

// CreateMember handles POST /members and answers with the handoff step.
func (h *MemberHandler) CreateMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	actor := middleware.UserFrom(ctx)

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	level := domain.LevelViewer
	if r.FormValue("as_maintainer") != "" {
		level = domain.LevelMaintainer
	}
	assignments := make([]domain.ProjectAssignment, 0)
	for _, raw := range r.Form["projects"] {
		if id, err := uuid.Parse(raw); err == nil {
			assignments = append(assignments, domain.ProjectAssignment{ProjectID: id, Level: level})
		}
	}

	opts := &domain.CreateMemberOpts{
		Name:     r.FormValue("name"),
		Email:    r.FormValue("email"),
		Role:     domain.Role(r.FormValue("role")),
		Projects: assignments,
	}

	created, password, _, err := h.memberSvc.CreateMember(ctx, actor, opts)
	if err != nil {
		// 200 with the form back: HTMX does not swap a non-2xx body, so an
		// error sent as 4xx would never appear.
		errs := fieldErrorsOf(err)
		if len(errs) == 0 {
			errs = []utils.FieldError{{Field: "form", Detail: err.Error()}}
		}
		view.AddMemberForm(view.AddMemberFormData{
			Name: opts.Name, Email: opts.Email, Role: opts.Role,
			Projects:        assignableProjects(r, h.projectSvc),
			CanCreateAdmins: actor != nil && actor.Role == domain.RoleSuperAdmin,
			Errors:          errs,
		}).Render(ctx, w)
		return
	}

	view.MemberCreated(created.Name, created.Email, password).Render(ctx, w)
	// The table behind the sheet refreshes out of band.
	view.MembersTableOOB(membersData(r, h.memberSvc, h.projectSvc)).Render(ctx, w)
}

// ResetPassword handles POST /members/{id}/reset.
func (h *MemberHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	targetID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		view.MembersError("Invalid account").Render(ctx, w)
		return
	}

	password, _, err := h.memberSvc.ResetMemberPassword(ctx, middleware.UserFrom(ctx), targetID)
	if err != nil {
		view.MembersError(err.Error()).Render(ctx, w)
		return
	}

	name := "this account"
	if u, lookupErr := h.memberSvc.GetMember(ctx, targetID); lookupErr == nil {
		name = u.Name
	}
	w.Header().Set("Cache-Control", "no-store")
	view.TempPasswordNotice(name, password).Render(ctx, w)
}

// ChangeRole handles POST /members/{id}/role and re-renders the table.
func (h *MemberHandler) ChangeRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	targetID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		view.MembersTable(membersData(r, h.memberSvc, h.projectSvc)).Render(ctx, w)
		return
	}
	if err := r.ParseForm(); err != nil {
		view.MembersTable(membersData(r, h.memberSvc, h.projectSvc)).Render(ctx, w)
		return
	}

	if _, err := h.memberSvc.ChangeRole(ctx, middleware.UserFrom(ctx), targetID, domain.Role(r.FormValue("role"))); err != nil {
		cslog.L(ctx).WithError(err).Warn("Role change refused")
	}
	view.MembersTable(membersData(r, h.memberSvc, h.projectSvc)).Render(ctx, w)
}

// DeleteMember handles POST /members/{id}/delete and re-renders the table.
func (h *MemberHandler) DeleteMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	targetID, err := uuid.Parse(mux.Vars(r)["id"])
	if err == nil {
		if _, err := h.memberSvc.DeleteMember(ctx, middleware.UserFrom(ctx), targetID); err != nil {
			cslog.L(ctx).WithError(err).Warn("Member removal refused")
		}
	}
	view.MembersTable(membersData(r, h.memberSvc, h.projectSvc)).Render(ctx, w)
}

// fieldErrorsOf pulls per-field messages out of a service error, if it carries
// any, so they can be rendered against the offending input.
func fieldErrorsOf(err error) []utils.FieldError {
	var appErr *utils.ErrorJson
	if errors.As(err, &appErr) {
		return appErr.Errors
	}
	return nil
}
