package adminapi

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

type createPlatformAdminRequest struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

type changePlatformAdminStateRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) registerUserManagementRoutes() {
	h.mux.HandleFunc("GET /api/v1/admin/users", h.listPlatformAdmins)
	h.mux.HandleFunc("POST /api/v1/admin/users", h.createPlatformAdmin)
	h.mux.HandleFunc("POST /api/v1/admin/users/{user_id}/disable", h.disablePlatformAdmin)
	h.mux.HandleFunc("POST /api/v1/admin/users/{user_id}/enable", h.enablePlatformAdmin)
}

func (h *Handler) listPlatformAdmins(w http.ResponseWriter, r *http.Request) {
	actorID, err := h.authorizeRead(r, platformAdminAuthorization)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	limit, offset, err := adminPage(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state != "" && state != "ACTIVE" && state != "DISABLED" {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "state must be ACTIVE or DISABLED"))
		return
	}

	rows, err := h.db.Query(r.Context(), `
		SELECT u.id,u.email,u.display_name,u.state,u.created_at,u.updated_at,
		       count(*) OVER()
		FROM app_users u
		JOIN platform_user_roles pur ON pur.user_id=u.id AND pur.role='PLATFORM_ADMIN'
		WHERE ($1='' OR u.display_name ILIKE '%'||$1||'%' OR u.email ILIKE '%'||$1||'%')
		  AND ($2='' OR u.state=$2)
		ORDER BY u.created_at DESC,u.id DESC
		LIMIT $3 OFFSET $4
	`, query, state, limit, offset)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	var total int64
	for rows.Next() {
		var id uuid.UUID
		var userState string
		var email, displayName *string
		var createdAt, updatedAt time.Time
		if err = rows.Scan(&id, &email, &displayName, &userState, &createdAt, &updatedAt, &total); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}
		items = append(items, map[string]any{
			"id":    publicid.Encode(publicid.User, id),
			"email": email, "display_name": displayName, "state": userState,
			"role": "PLATFORM_ADMIN", "is_current_user": id == actorID,
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	if err = rows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) createPlatformAdmin(w http.ResponseWriter, r *http.Request) {
	var request createPlatformAdminRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	if request.DisplayName == "" || len(request.DisplayName) > 160 {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "display_name must be between 1 and 160 characters"))
		return
	}
	address, err := mail.ParseAddress(request.Email)
	if err != nil || !strings.EqualFold(address.Address, request.Email) || len(request.Email) > 254 {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "email must be a valid email address"))
		return
	}
	if _, err = h.authorizeRead(r, platformAdminAuthorization); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	if h.identityAdmin == nil {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "administrator invitations are not configured"))
		return
	}
	identity, invitationSent, err := h.identityAdmin.EnsureInvited(r.Context(), request.Email, request.DisplayName)
	if err != nil {
		if errors.Is(err, errIdentityInviteRejected) {
			httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "this email address could not be invited"))
			return
		}
		httpserver.WriteError(w, r, apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "administrator invitation service is temporarily unavailable"))
		return
	}

	h.runMutation(w, r, "ADMIN_USER_CREATE", request.Email, request, platformAdminAuthorization, false,
		func(ctx context.Context, actorID uuid.UUID) (response, error) {
			tx, ok := database.TransactionFromContext(ctx)
			if !ok {
				return response{}, errors.New("admin user mutation requires a transaction")
			}
			userID := uuid.New()
			var previousState string
			previousFound := true
			err := tx.QueryRow(ctx, `
				SELECT state FROM app_users
				WHERE auth_provider='supabase' AND auth_subject=$1
				FOR UPDATE
			`, identity.ID.String()).Scan(&previousState)
			if errors.Is(err, pgx.ErrNoRows) {
				previousFound = false
			} else if err != nil {
				return response{}, err
			}
			var conflictingUser uuid.UUID
			err = tx.QueryRow(ctx, `SELECT id FROM app_users WHERE lower(email)=lower($1) AND auth_subject<>$2`, request.Email, identity.ID.String()).Scan(&conflictingUser)
			if err == nil {
				return response{}, apierror.WithStatus(apierror.CodeValidation, "this email is already assigned to another TktSync user", http.StatusConflict)
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return response{}, err
			}
			err = tx.QueryRow(ctx, `
				INSERT INTO app_users(id,auth_provider,auth_subject,email,display_name,state,created_at,updated_at)
				VALUES($1,'supabase',$2,$3,$4,'ACTIVE',clock_timestamp(),clock_timestamp())
				ON CONFLICT(auth_provider,auth_subject) DO UPDATE
				SET email=EXCLUDED.email,display_name=EXCLUDED.display_name,state='ACTIVE',updated_at=clock_timestamp()
				RETURNING id
			`, userID, identity.ID.String(), request.Email, request.DisplayName).Scan(&userID)
			if err != nil {
				return response{}, err
			}
			var roleGranted string
			err = tx.QueryRow(ctx, `
				INSERT INTO platform_user_roles(user_id,role,created_at)
				VALUES($1,'PLATFORM_ADMIN',clock_timestamp())
				ON CONFLICT(user_id,role) DO NOTHING
				RETURNING role
			`, userID).Scan(&roleGranted)
			if errors.Is(err, pgx.ErrNoRows) {
				return response{}, apierror.WithStatus(apierror.CodeValidation, "this user is already a Platform Admin", http.StatusConflict)
			}
			if err != nil {
				return response{}, err
			}
			previous := map[string]any(nil)
			if previousFound {
				previous = map[string]any{"state": previousState, "role": nil}
			}
			if _, err = (audit.Store{}).Append(ctx, tx, audit.Event{
				ActorKind: audit.ActorUser, ActorUserID: &actorID, Operation: "PLATFORM_ADMIN_CREATED",
				EntityType: "APP_USER", EntityID: &userID,
				PreviousState: previous,
				NewState:      map[string]any{"state": "ACTIVE", "role": "PLATFORM_ADMIN", "auth_provider": "supabase", "display_name": request.DisplayName, "invitation_sent": invitationSent},
			}); err != nil {
				return response{}, err
			}
			return jsonResponse(http.StatusCreated, map[string]any{
				"id":    publicid.Encode(publicid.User, userID),
				"email": request.Email, "display_name": request.DisplayName,
				"state": "ACTIVE", "role": "PLATFORM_ADMIN", "is_current_user": false,
				"invitation_sent": invitationSent,
			})
		})
}

func (h *Handler) disablePlatformAdmin(w http.ResponseWriter, r *http.Request) {
	h.changePlatformAdminState(w, r, "DISABLED")
}

func (h *Handler) enablePlatformAdmin(w http.ResponseWriter, r *http.Request) {
	h.changePlatformAdminState(w, r, "ACTIVE")
}

func (h *Handler) changePlatformAdminState(w http.ResponseWriter, r *http.Request, nextState string) {
	targetID, err := publicid.Parse(strings.TrimSpace(r.PathValue("user_id")), publicid.User)
	if err != nil {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "user_id is invalid"))
		return
	}
	var request changePlatformAdminStateRequest
	if err = decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 500 {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "reason must be between 1 and 500 characters"))
		return
	}
	operation := "ADMIN_USER_ENABLE"
	auditOperation := "PLATFORM_ADMIN_ENABLED"
	if nextState == "DISABLED" {
		operation = "ADMIN_USER_DISABLE"
		auditOperation = "PLATFORM_ADMIN_DISABLED"
	}

	h.runMutation(w, r, operation, targetID.String(), request, platformAdminAuthorization, false,
		func(ctx context.Context, actorID uuid.UUID) (response, error) {
			tx, ok := database.TransactionFromContext(ctx)
			if !ok {
				return response{}, errors.New("admin user mutation requires a transaction")
			}
			if nextState == "DISABLED" && targetID == actorID {
				return response{}, apierror.New(apierror.CodeValidation, "you cannot disable your own administrator access")
			}
			var previousState string
			err := tx.QueryRow(ctx, `
				SELECT u.state FROM app_users u
				JOIN platform_user_roles pur ON pur.user_id=u.id AND pur.role='PLATFORM_ADMIN'
				WHERE u.id=$1 FOR UPDATE OF u
			`, targetID).Scan(&previousState)
			if errors.Is(err, pgx.ErrNoRows) {
				return response{}, apierror.New(apierror.CodeResourceNotFound, "Platform Admin not found")
			}
			if err != nil {
				return response{}, err
			}
			if previousState != nextState {
				if _, err = tx.Exec(ctx, `UPDATE app_users SET state=$2,updated_at=clock_timestamp() WHERE id=$1`, targetID, nextState); err != nil {
					return response{}, err
				}
				if _, err = (audit.Store{}).Append(ctx, tx, audit.Event{
					ActorKind: audit.ActorUser, ActorUserID: &actorID, Operation: auditOperation,
					EntityType: "APP_USER", EntityID: &targetID, Reason: request.Reason,
					PreviousState: map[string]any{"state": previousState, "role": "PLATFORM_ADMIN"},
					NewState:      map[string]any{"state": nextState, "role": "PLATFORM_ADMIN"},
				}); err != nil {
					return response{}, err
				}
			}
			return jsonResponse(http.StatusOK, map[string]any{"id": publicid.Encode(publicid.User, targetID), "state": nextState})
		})
}
