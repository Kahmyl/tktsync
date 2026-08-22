package partnerapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/idempotency"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
)

type partnerMutationResponse struct {
	Status                  int
	Body                    json.RawMessage
	EntityID                *uuid.UUID
	RecoverReservationToken bool
	RecoverSelectionURL     bool
	EntityType              string
}

type persistedPartnerResponse struct {
	Status                  int             `json:"status"`
	Body                    json.RawMessage `json:"body"`
	RecoverReservationToken bool            `json:"recover_reservation_token,omitempty"`
	RecoverSelectionURL     bool            `json:"recover_selection_url,omitempty"`
}

type persistedPartnerFailure struct {
	Message    string         `json:"message"`
	Details    map[string]any `json:"details,omitempty"`
	HTTPStatus int            `json:"http_status"`
}

type partnerMutationFunc func(
	context.Context,
	auth.PartnerPrincipal,
) (partnerMutationResponse, error)

func (h *Handler) authenticatePartner(
	r *http.Request,
) (auth.PartnerPrincipal, error) {
	header := strings.TrimSpace(
		r.Header.Get("Authorization"),
	)

	const prefix = "Bearer "

	if !strings.HasPrefix(
		header,
		prefix,
	) {
		return auth.PartnerPrincipal{},
			apierror.WithStatus(
				apierror.CodeNotAuthorized,
				"authentication is required",
				http.StatusUnauthorized,
			)
	}

	raw := strings.TrimSpace(
		strings.TrimPrefix(
			header,
			prefix,
		),
	)

	if raw == "" {
		return auth.PartnerPrincipal{},
			apierror.WithStatus(
				apierror.CodeNotAuthorized,
				"authentication is required",
				http.StatusUnauthorized,
			)
	}

	return h.partnerAuth.Authenticate(
		r.Context(),
		raw,
	)
}

func reservationTokenIntentHash(
	raw string,
) string {
	sum := sha256.Sum256(
		[]byte(
			strings.TrimSpace(raw),
		),
	)

	return hex.EncodeToString(
		sum[:],
	)
}

func canonicalPartnerMutation(
	operation string,
	pathIdentity string,
	request any,
	reservationToken string,
) ([]byte, error) {
	body, err := json.Marshal(
		request,
	)
	if err != nil {
		return nil, err
	}

	value := operation +
		"\n" +
		pathIdentity +
		"\n" +
		string(body)

	if strings.TrimSpace(
		reservationToken,
	) != "" {
		value += "\nreservation_token_sha256=" +
			reservationTokenIntentHash(
				reservationToken,
			)
	}

	return []byte(value), nil
}

func materializedBusinessError(
	code apierror.Code,
) bool {
	switch code {
	case apierror.CodeHoldExpired,
		apierror.CodeCheckoutWindowExpired,
		apierror.CodeReconciliationExpired,
		apierror.CodePaymentStatusUncertain,
		apierror.CodeEventCancelled:
		return true

	default:
		return false
	}
}

func stripReservationToken(
	raw json.RawMessage,
) (json.RawMessage, error) {
	var body map[string]any

	if err := json.Unmarshal(
		raw,
		&body,
	); err != nil {
		return nil, err
	}

	delete(
		body,
		"reservation_token",
	)

	return json.Marshal(body)
}

func injectReservationToken(
	raw json.RawMessage,
	token string,
) (json.RawMessage, error) {
	var body map[string]any

	if err := json.Unmarshal(
		raw,
		&body,
	); err != nil {
		return nil, err
	}

	body["reservation_token"] =
		token

	return json.Marshal(body)
}

func (h *Handler) runPartnerMutation(
	w http.ResponseWriter,
	r *http.Request,
	operation string,
	canonical []byte,
	mutate partnerMutationFunc,
) {
	if h.transactions == nil ||
		h.reservation == nil {
		httpserver.WriteError(
			w,
			r,
			apierror.New(
				apierror.CodeAuthorityTemporarilyUnavailable,
				"Reservation API is not configured",
			),
		)
		return
	}

	principal, err :=
		h.authenticatePartner(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	idempotencyKey :=
		strings.TrimSpace(
			r.Header.Get(
				"Idempotency-Key",
			),
		)

	if idempotencyKey == "" {
		httpserver.WriteError(
			w,
			r,
			apierror.New(
				apierror.CodeValidation,
				"Idempotency-Key header is required",
			),
		)
		return
	}

	requestHash :=
		idempotency.Fingerprint(
			canonical,
		)

	store := idempotency.Store{}

	var (
		finalResponse partnerMutationResponse
		businessErr   error
	)

	err = h.transactions.Run(
		r.Context(),
		func(tx pgx.Tx) error {
			claim, err := store.Claim(
				r.Context(),
				tx,
				idempotency.Scope{
					Kind: idempotency.ScopePartner,
					ID:   principal.PartnerID,
				},
				operation,
				idempotencyKey,
				requestHash,
			)
			if err != nil {
				return err
			}

			if !claim.Owner {
				if claim.Replay == nil {
					return errors.New(
						"idempotency replay result is missing",
					)
				}

				if claim.Replay.
					ExecutionState ==
					"FAILED_BUSINESS" {
					var stored persistedPartnerFailure

					if len(
						claim.Replay.Payload,
					) != 0 {
						if err := json.Unmarshal(
							claim.Replay.Payload,
							&stored,
						); err != nil {
							return err
						}
					}

					replayed := apierror.New(
						apierror.Code(
							claim.Replay.Code,
						),
						stored.Message,
					)

					if stored.HTTPStatus != 0 {
						replayed.HTTPStatus =
							stored.HTTPStatus
					}

					replayed.Details =
						stored.Details

					businessErr = replayed
					return nil
				}

				var stored persistedPartnerResponse

				if err := json.Unmarshal(
					claim.Replay.Payload,
					&stored,
				); err != nil {
					return err
				}

				body := stored.Body

				if stored.
					RecoverReservationToken {
					if claim.Replay.EntityID ==
						nil {
						return errors.New(
							"Reservation replay identity is missing",
						)
					}

					token, err :=
						h.reservation.RecoverToken(
							database.WithTransaction(
								r.Context(),
								tx,
							),
							*claim.Replay.EntityID,
							principal.PartnerID,
						)
					if err != nil {
						return err
					}

					body, err =
						injectReservationToken(
							body,
							token,
						)
					if err != nil {
						return err
					}
				}
				if stored.RecoverSelectionURL {
					if claim.Replay.EntityID == nil || h.selection == nil {
						return errors.New("Selection session replay identity is missing")
					}
					recovered, recoverErr := h.selection.Recover(r.Context(), *claim.Replay.EntityID, principal.PartnerID)
					if recoverErr != nil {
						return recoverErr
					}
					var decoded map[string]any
					if err := json.Unmarshal(body, &decoded); err != nil {
						return err
					}
					decoded["selection_url"] = recovered.SelectionURL
					body, err = json.Marshal(decoded)
					if err != nil {
						return err
					}
				}

				finalResponse =
					partnerMutationResponse{
						Status: stored.Status,
						Body:   body,
						EntityID: claim.
							Replay.EntityID,
						RecoverReservationToken: stored.
							RecoverReservationToken,
						RecoverSelectionURL: stored.RecoverSelectionURL,
					}

				return nil
			}

			businessTx, err :=
				tx.Begin(
					r.Context(),
				)
			if err != nil {
				return err
			}

			businessCtx :=
				database.WithTransaction(
					r.Context(),
					businessTx,
				)

			result, err := mutate(
				businessCtx,
				principal,
			)
			if err != nil {
				apiErr, ok :=
					apierror.As(err)
				if !ok {
					_ = businessTx.Rollback(
						r.Context(),
					)
					return err
				}

				if materializedBusinessError(
					apiErr.Code,
				) {
					if err :=
						businessTx.Commit(
							r.Context(),
						); err != nil {
						return err
					}
				} else {
					if err :=
						businessTx.Rollback(
							r.Context(),
						); err != nil {
						return err
					}
				}

				stored :=
					persistedPartnerFailure{
						Message:    apiErr.Message,
						Details:    apiErr.Details,
						HTTPStatus: apiErr.HTTPStatus,
					}

				if err := store.
					CompleteBusinessFailure(
						r.Context(),
						tx,
						claim.ID,
						string(apiErr.Code),
						stored,
					); err != nil {
					return err
				}

				businessErr = apiErr
				return nil
			}

			if err :=
				businessTx.Commit(
					r.Context(),
				); err != nil {
				return err
			}

			storedBody := result.Body

			if result.
				RecoverReservationToken {
				storedBody, err =
					stripReservationToken(
						result.Body,
					)
				if err != nil {
					return err
				}
			}

			stored :=
				persistedPartnerResponse{
					Status: result.Status,
					Body:   storedBody,
					RecoverReservationToken: result.
						RecoverReservationToken,
					RecoverSelectionURL: result.RecoverSelectionURL,
				}
			if result.RecoverSelectionURL {
				var decoded map[string]any
				if err := json.Unmarshal(stored.Body, &decoded); err != nil {
					return err
				}
				delete(decoded, "selection_url")
				stored.Body, err = json.Marshal(decoded)
				if err != nil {
					return err
				}
			}

			entityType := result.EntityType
			if result.EntityID != nil {
				if entityType == "" {
					entityType = "RESERVATION"
				}
			}

			if err := store.CompleteSuccess(
				r.Context(),
				tx,
				claim.ID,
				"OK",
				entityType,
				result.EntityID,
				stored,
			); err != nil {
				return err
			}

			finalResponse = result
			return nil
		},
	)
	if err != nil {
		httpserver.WriteError(
			w,
			r,
			err,
		)
		return
	}

	if businessErr != nil {
		httpserver.WriteError(
			w,
			r,
			businessErr,
		)
		return
	}

	if finalResponse.RecoverReservationToken || finalResponse.RecoverSelectionURL {
		w.Header().Set(
			"Cache-Control",
			"no-store",
		)
	}

	if len(finalResponse.Body) == 0 {
		finalResponse.Body =
			json.RawMessage(`{}`)
	}

	httpserver.WriteJSON(
		w,
		finalResponse.Status,
		finalResponse.Body,
	)
}

func partnerJSONResponse(
	status int,
	value any,
	entityID *uuid.UUID,
	recoverReservationToken bool,
) (partnerMutationResponse, error) {
	raw, err := json.Marshal(
		value,
	)
	if err != nil {
		return partnerMutationResponse{},
			err
	}

	return partnerMutationResponse{
		Status:                  status,
		Body:                    raw,
		EntityID:                entityID,
		RecoverReservationToken: recoverReservationToken,
	}, nil
}
