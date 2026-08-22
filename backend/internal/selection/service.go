package selection

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

const defaultLifetime = 30 * time.Minute

type Session struct {
	ID              uuid.UUID
	PartnerID       uuid.UUID
	EventID         uuid.UUID
	BuyerSessionRef string
	ReturnURL       string
	State           string
	ExpiresAt       time.Time
}

type Created struct {
	Session
	Capability   string
	SelectionURL string
}

type Service struct {
	db           *pgxpool.Pool
	transactions *database.Runner
	keys         *auth.HMACKeyring
	selectorURL  string
	audit        audit.Store
	outbox       outbox.Store
}

func NewService(db *pgxpool.Pool, transactions *database.Runner, keys *auth.HMACKeyring, selectorURL string) *Service {
	return &Service{db: db, transactions: transactions, keys: keys, selectorURL: strings.TrimSpace(selectorURL)}
}

func (s *Service) Create(ctx context.Context, partnerID, eventID uuid.UUID, buyerRef, returnURL string) (Created, error) {
	if s.keys == nil || s.transactions == nil {
		return Created{}, apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "Selection capability authority is not configured")
	}
	parsed, err := url.Parse(strings.TrimSpace(returnURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return Created{}, apierror.New(apierror.CodeValidation, "return_url must be an absolute registered HTTPS URL")
	}
	returnURL = parsed.String()
	id := uuid.New()
	version := s.keys.ActiveVersion()
	var result Created
	err = s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var partnerState, accessState string
		var allowed bool
		if err := tx.QueryRow(ctx, `SELECT p.state,pea.state,COALESCE(p.metadata->'allowed_return_urls' ? $3,false) FROM partners p JOIN partner_event_access pea ON pea.partner_id=p.id AND pea.event_id=$2 WHERE p.id=$1 FOR KEY SHARE OF p,pea`, partnerID, eventID, returnURL).Scan(&partnerState, &accessState, &allowed); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierror.New(apierror.CodeNotAuthorized, "Partner is not authorized for this Event")
			}
			return err
		}
		if partnerState != "ACTIVE" || accessState != "ACTIVE" {
			return apierror.New(apierror.CodeNotAuthorized, "Partner Event access is not active")
		}
		if !allowed {
			return apierror.New(apierror.CodeValidation, "return_url is not registered for this Partner")
		}
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		expiresAt := now.Add(defaultLifetime)
		capability, err := s.build(id, partnerID, eventID, expiresAt, version)
		if err != nil {
			return err
		}
		hash := auth.TokenHash(capability)
		if _, err = tx.Exec(ctx, `INSERT INTO buyer_selection_sessions(id,partner_id,event_id,token_hash,token_key_version,buyer_session_ref,state,expires_at,created_at,return_url) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),'ACTIVE',$7,$8,$9)`, id, partnerID, eventID, hash[:], version, strings.TrimSpace(buyerRef), expiresAt, now, returnURL); err != nil {
			return err
		}
		if _, err = s.audit.Append(ctx, tx, audit.Event{PartnerID: &partnerID, ActorKind: audit.ActorPartner, ActorPartnerID: &partnerID, Operation: "BUYER_SELECTION_SESSION_CREATED", EntityType: "BUYER_SELECTION_SESSION", EntityID: &id, NewState: map[string]any{"state": "ACTIVE", "expires_at": expiresAt}}); err != nil {
			return err
		}
		if _, err = s.outbox.Append(ctx, tx, outbox.Fact{EventID: &eventID, FactType: "selection.session_created", AggregateType: "BUYER_SELECTION_SESSION", AggregateID: &id, Payload: map[string]any{"partner_id": partnerID.String()}}); err != nil {
			return err
		}
		result = Created{Session: Session{ID: id, PartnerID: partnerID, EventID: eventID, BuyerSessionRef: strings.TrimSpace(buyerRef), ReturnURL: returnURL, State: "ACTIVE", ExpiresAt: expiresAt}, Capability: capability, SelectionURL: s.selectorURL + "#" + capability}
		return nil
	})
	return result, err
}

func (s *Service) Recover(ctx context.Context, id, partnerID uuid.UUID) (Created, error) {
	var session Session
	var version int
	if s.keys == nil {
		return Created{}, apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "Selection capability authority is not configured")
	}
	err := s.db.QueryRow(ctx, `SELECT id,partner_id,event_id,COALESCE(buyer_session_ref,''),return_url,state,expires_at,token_key_version FROM buyer_selection_sessions WHERE id=$1 AND partner_id=$2`, id, partnerID).Scan(&session.ID, &session.PartnerID, &session.EventID, &session.BuyerSessionRef, &session.ReturnURL, &session.State, &session.ExpiresAt, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Created{}, apierror.New(apierror.CodeResourceNotFound, "Selection session not found")
	}
	if err != nil {
		return Created{}, err
	}
	capability, err := s.build(session.ID, session.PartnerID, session.EventID, session.ExpiresAt, version)
	if err != nil {
		return Created{}, err
	}
	return Created{Session: session, Capability: capability, SelectionURL: s.selectorURL + "#" + capability}, nil
}

func (s *Service) Authenticate(ctx context.Context, raw string) (Session, error) {
	if s.keys == nil || s.db == nil {
		return Session{}, apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "Selection capability authority is not configured")
	}
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 4 || parts[0] != "sel1" {
		return Session{}, unauthorized()
	}
	version, err := strconv.Atoi(parts[1])
	if err != nil || version <= 0 {
		return Session{}, unauthorized()
	}
	id, err := uuid.Parse(parts[2])
	if err != nil {
		return Session{}, unauthorized()
	}
	presented, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(presented) != 32 {
		return Session{}, unauthorized()
	}
	var session Session
	var storedVersion int
	var tokenHash []byte
	var serverTime time.Time
	err = s.db.QueryRow(ctx, `SELECT s.id,s.partner_id,s.event_id,COALESCE(s.buyer_session_ref,''),s.return_url,s.state,s.expires_at,s.token_key_version,s.token_hash,clock_timestamp() FROM buyer_selection_sessions s JOIN partners p ON p.id=s.partner_id JOIN partner_event_access pea ON pea.partner_id=s.partner_id AND pea.event_id=s.event_id WHERE s.id=$1 AND p.state='ACTIVE' AND pea.state='ACTIVE'`, id).Scan(&session.ID, &session.PartnerID, &session.EventID, &session.BuyerSessionRef, &session.ReturnURL, &session.State, &session.ExpiresAt, &storedVersion, &tokenHash, &serverTime)
	if err != nil || session.State != "ACTIVE" || !serverTime.Before(session.ExpiresAt) || storedVersion != version {
		return Session{}, unauthorized()
	}
	hash := auth.TokenHash(raw)
	if subtle.ConstantTimeCompare(tokenHash, hash[:]) != 1 || !s.keys.Verify(version, selectionMessage(version, session), presented) {
		return Session{}, unauthorized()
	}
	return session, nil
}

func (s *Service) build(id, partnerID, eventID uuid.UUID, expiresAt time.Time, version int) (string, error) {
	session := Session{ID: id, PartnerID: partnerID, EventID: eventID, ExpiresAt: expiresAt}
	mac, err := s.keys.MAC(version, selectionMessage(version, session))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sel1.%d.%s.%s", version, id.String(), base64.RawURLEncoding.EncodeToString(mac)), nil
}

func selectionMessage(version int, session Session) []byte {
	return auth.Canonical("sel1", strconv.Itoa(version), session.ID.String(), session.PartnerID.String(), session.EventID.String(), session.ExpiresAt.UTC().Format(time.RFC3339Nano))
}

func unauthorized() error {
	return apierror.WithStatus(apierror.CodeNotAuthorized, "Selection capability is invalid or expired", 401)
}
