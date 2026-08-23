package webhook

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

type Service struct {
	transactions *database.Runner
	box          *SecretBox
	keyVersion   int
	allowPrivate bool
	audit        audit.Store
}

func NewService(transactions *database.Runner, box *SecretBox, keyVersion int, allowPrivate bool) *Service {
	return &Service{transactions: transactions, box: box, keyVersion: keyVersion, allowPrivate: allowPrivate, audit: audit.Store{}}
}

type Endpoint struct {
	ID            uuid.UUID
	PartnerID     uuid.UUID
	URL           string
	State         string
	Secret        string
	Subscriptions []string
	CreatedAt     time.Time
}

func normalizeSubscriptions(values []string) []string {
	set := map[string]struct{}{}
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			set[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func validateDestination(raw string, allowPrivate bool) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return "", apierror.New(apierror.CodeValidation, "webhook URL is invalid")
	}
	if parsed.Scheme != "https" && !(allowPrivate && parsed.Scheme == "http") {
		return "", apierror.New(apierror.CodeValidation, "webhook URL must use HTTPS")
	}
	if parsed.Fragment != "" {
		return "", apierror.New(apierror.CodeValidation, "webhook URL cannot contain a fragment")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && !allowPrivate && unsafeIP(ip) {
		return "", apierror.New(apierror.CodeValidation, "webhook URL cannot target a private or reserved address")
	}
	return parsed.String(), nil
}

var blockedWebhookPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func unsafeIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}

	addr = addr.Unmap()

	if !addr.IsValid() ||
		addr.IsUnspecified() ||
		addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() ||
		addr.IsMulticast() {
		return true
	}

	for _, prefix := range blockedWebhookPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}

	return false
}

func (s *Service) CreateEndpoint(ctx context.Context, actorID, partnerID uuid.UUID, rawURL string, subscriptions []string) (Endpoint, error) {
	if actorID == uuid.Nil || partnerID == uuid.Nil {
		return Endpoint{}, apierror.New(apierror.CodeValidation, "Actor and Partner are required")
	}
	if s.box == nil || s.keyVersion <= 0 {
		return Endpoint{}, apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "webhook secret encryption is not configured")
	}
	destination, err := validateDestination(rawURL, s.allowPrivate)
	if err != nil {
		return Endpoint{}, err
	}
	subscriptions = normalizeSubscriptions(subscriptions)
	if len(subscriptions) == 0 {
		return Endpoint{}, apierror.New(apierror.CodeValidation, "at least one webhook subscription is required")
	}
	rawSecret := make([]byte, 32)
	if _, err = rand.Read(rawSecret); err != nil {
		return Endpoint{}, err
	}
	ciphertext, err := s.box.SealVersion(s.keyVersion, rawSecret)
	if err != nil {
		return Endpoint{}, err
	}
	secret := base64.RawURLEncoding.EncodeToString(rawSecret)
	var result Endpoint
	err = s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var partnerState string
		if err := tx.QueryRow(ctx, `SELECT state FROM partners WHERE id=$1 FOR KEY SHARE`, partnerID).Scan(&partnerState); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierror.New(apierror.CodeResourceNotFound, "Partner not found")
			}
			return err
		}
		endpointID, secretID := uuid.New(), uuid.New()
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO partner_webhook_endpoints(id,partner_id,url,state,created_at,updated_at)VALUES($1,$2,$3,'ACTIVE',$4,$4)`, endpointID, partnerID, destination, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO partner_webhook_signing_secrets(id,webhook_endpoint_id,secret_ciphertext,encryption_key_version,state,activated_at,created_at)VALUES($1,$2,$3,$4,'ACTIVE',$5,$5)`, secretID, endpointID, ciphertext, s.keyVersion, now); err != nil {
			return err
		}
		for _, eventType := range subscriptions {
			if _, err := tx.Exec(ctx, `INSERT INTO partner_webhook_subscriptions(webhook_endpoint_id,event_type)VALUES($1,$2)`, endpointID, eventType); err != nil {
				return err
			}
		}
		if _, err := s.audit.Append(ctx, tx, audit.Event{PartnerID: &partnerID, ActorKind: audit.ActorUser, ActorUserID: &actorID, Operation: "WEBHOOK_ENDPOINT_CREATED", EntityType: "WEBHOOK_ENDPOINT", EntityID: &endpointID, NewState: map[string]any{"state": "ACTIVE", "url": destination, "subscriptions": subscriptions}}); err != nil {
			return err
		}
		result = Endpoint{ID: endpointID, PartnerID: partnerID, URL: destination, State: "ACTIVE", Secret: secret, Subscriptions: subscriptions, CreatedAt: now}
		return nil
	})
	if err != nil {
		return Endpoint{}, err
	}
	return result, nil
}

func (s *Service) RotateSecret(ctx context.Context, actorID, endpointID uuid.UUID) (string, time.Time, error) {
	if actorID == uuid.Nil || endpointID == uuid.Nil {
		return "", time.Time{}, apierror.New(apierror.CodeValidation, "Actor and endpoint are required")
	}
	if s.box == nil || s.keyVersion <= 0 {
		return "", time.Time{}, apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "webhook secret encryption is not configured")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	ciphertext, err := s.box.SealVersion(s.keyVersion, raw)
	if err != nil {
		return "", time.Time{}, err
	}
	var activated time.Time
	err = s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var partnerID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT partner_id FROM partner_webhook_endpoints WHERE id=$1 FOR UPDATE`, endpointID).Scan(&partnerID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&activated); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE partner_webhook_signing_secrets SET state='RETIRING',valid_until=$2 WHERE webhook_endpoint_id=$1 AND state='ACTIVE'`, endpointID, activated.Add(24*time.Hour)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO partner_webhook_signing_secrets(id,webhook_endpoint_id,secret_ciphertext,encryption_key_version,state,activated_at,created_at)VALUES($1,$2,$3,$4,'ACTIVE',$5,$5)`, uuid.New(), endpointID, ciphertext, s.keyVersion, activated); err != nil {
			return err
		}
		_, err := s.audit.Append(ctx, tx, audit.Event{PartnerID: &partnerID, ActorKind: audit.ActorUser, ActorUserID: &actorID, Operation: "WEBHOOK_SECRET_ROTATED", EntityType: "WEBHOOK_ENDPOINT", EntityID: &endpointID})
		return err
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return base64.RawURLEncoding.EncodeToString(raw), activated, nil
}

func (s *Service) DisableEndpoint(ctx context.Context, actorID, endpointID uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var partnerID uuid.UUID
		tag, err := tx.Exec(ctx, `UPDATE partner_webhook_endpoints SET state='DISABLED',disabled_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 AND state='ACTIVE'`, endpointID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return apierror.New(apierror.CodeResourceNotFound, "Active webhook endpoint not found")
		}
		if err := tx.QueryRow(ctx, `SELECT partner_id FROM partner_webhook_endpoints WHERE id=$1`, endpointID).Scan(&partnerID); err != nil {
			return err
		}
		_, err = s.audit.Append(ctx, tx, audit.Event{PartnerID: &partnerID, ActorKind: audit.ActorUser, ActorUserID: &actorID, Operation: "WEBHOOK_ENDPOINT_DISABLED", EntityType: "WEBHOOK_ENDPOINT", EntityID: &endpointID, Reason: reason, NewState: map[string]any{"state": "DISABLED"}})
		return err
	})
}

func (s *Service) ReplaceSubscriptions(ctx context.Context, actorID, endpointID uuid.UUID, subscriptions []string) error {
	subscriptions = normalizeSubscriptions(subscriptions)
	if len(subscriptions) == 0 {
		return apierror.New(apierror.CodeValidation, "at least one webhook subscription is required")
	}
	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var partnerID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT partner_id FROM partner_webhook_endpoints WHERE id=$1 FOR UPDATE`, endpointID).Scan(&partnerID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM partner_webhook_subscriptions WHERE webhook_endpoint_id=$1`, endpointID); err != nil {
			return err
		}
		for _, v := range subscriptions {
			if _, err := tx.Exec(ctx, `INSERT INTO partner_webhook_subscriptions(webhook_endpoint_id,event_type)VALUES($1,$2)`, endpointID, v); err != nil {
				return err
			}
		}
		_, err := s.audit.Append(ctx, tx, audit.Event{PartnerID: &partnerID, ActorKind: audit.ActorUser, ActorUserID: &actorID, Operation: "WEBHOOK_SUBSCRIPTIONS_REPLACED", EntityType: "WEBHOOK_ENDPOINT", EntityID: &endpointID, NewState: map[string]any{"subscriptions": subscriptions}})
		return err
	})
}
