package reporting

import (
	"bytes"
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Observer struct {
	requests       atomic.Uint64
	errors         atomic.Uint64
	authAnomalies  atomic.Uint64
	conflicts      atomic.Uint64
	holdConflicts  atomic.Uint64
	totalLatencyNS atomic.Uint64
	maxLatencyNS   atomic.Uint64
}

type RequestMetrics struct {
	RequestCount          uint64  `json:"request_count"`
	ErrorCount            uint64  `json:"error_count"`
	AuthAnomalyCount      uint64  `json:"auth_anomaly_count"`
	ConflictResponseCount uint64  `json:"conflict_response_count"`
	HoldConflictCount     uint64  `json:"hold_conflict_count"`
	ErrorRate             float64 `json:"error_rate"`
	HoldConflictRate      float64 `json:"hold_conflict_rate"`
	AverageLatencyMS      float64 `json:"average_latency_ms"`
	MaximumLatencyMS      float64 `json:"maximum_latency_ms"`
}

func NewObserver() *Observer { return &Observer{} }

type statusWriter struct {
	http.ResponseWriter
	status       int
	holdConflict bool
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if bytes.Contains(body, []byte(`"code":"INVENTORY_UNAVAILABLE"`)) || bytes.Contains(body, []byte(`"code":"INSUFFICIENT_GA_QUANTITY"`)) {
		w.holdConflict = true
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (o *Observer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		o.requests.Add(1)
		if status >= 400 {
			o.errors.Add(1)
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			o.authAnomalies.Add(1)
		}
		if status == http.StatusConflict {
			o.conflicts.Add(1)
		}
		if recorder.holdConflict {
			o.holdConflicts.Add(1)
		}
		latency := uint64(time.Since(started))
		o.totalLatencyNS.Add(latency)
		for {
			current := o.maxLatencyNS.Load()
			if latency <= current || o.maxLatencyNS.CompareAndSwap(current, latency) {
				break
			}
		}
	})
}

func (o *Observer) Snapshot() RequestMetrics {
	count := o.requests.Load()
	average := float64(0)
	errorRate := float64(0)
	holdConflictRate := float64(0)
	if count > 0 {
		average = float64(o.totalLatencyNS.Load()) / float64(count) / float64(time.Millisecond)
		errorRate = float64(o.errors.Load()) / float64(count)
		holdConflictRate = float64(o.holdConflicts.Load()) / float64(count)
	}
	return RequestMetrics{RequestCount: count, ErrorCount: o.errors.Load(), AuthAnomalyCount: o.authAnomalies.Load(), ConflictResponseCount: o.conflicts.Load(), HoldConflictCount: o.holdConflicts.Load(), ErrorRate: errorRate, HoldConflictRate: holdConflictRate, AverageLatencyMS: average, MaximumLatencyMS: float64(o.maxLatencyNS.Load()) / float64(time.Millisecond)}
}

type Alert struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Value    int64  `json:"value"`
}
type OperationalMetrics struct {
	GeneratedAt            time.Time        `json:"generated_at"`
	Event                  EventContext     `json:"event"`
	Requests               RequestMetrics   `json:"requests"`
	ReservationStates      map[string]int64 `json:"reservation_states"`
	ConfirmedSales         int64            `json:"confirmed_sales"`
	ConfirmationRate       float64          `json:"confirmation_rate"`
	OverdueReconciliations int64            `json:"overdue_reconciliations"`
	DueReservationWork     int64            `json:"due_reservation_work"`
	OldestWorkerLagSeconds int64            `json:"oldest_worker_lag_seconds"`
	PendingOutbox          int64            `json:"pending_outbox"`
	OldestOutboxLagSeconds int64            `json:"oldest_outbox_lag_seconds"`
	WebhookFailures        int64            `json:"webhook_failures"`
	WebhookDeadLetters     int64            `json:"webhook_dead_letters"`
	WaitingDatabaseLocks   int64            `json:"waiting_database_locks"`
	ScanOutcomes           map[string]int64 `json:"scan_outcomes"`
	Alerts                 []Alert          `json:"alerts"`
	Authority              string           `json:"authority"`
}

func (s *Service) Metrics(ctx context.Context, eventID uuid.UUID, observer *Observer) (OperationalMetrics, error) {
	result := OperationalMetrics{ReservationStates: map[string]int64{}, ScanOutcomes: map[string]int64{}, Alerts: []Alert{}, Authority: "ADVISORY_DERIVED_READ"}
	if observer != nil {
		result.Requests = observer.Snapshot()
	}
	err := s.readSnapshot(ctx, func(tx pgx.Tx) error {
		var err error
		result.Event, result.GeneratedAt, err = eventContext(ctx, tx, eventID)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT state,COUNT(*) FROM reservations WHERE event_id=$1 GROUP BY state ORDER BY state`, eventID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var state string
			var count int64
			if err := rows.Scan(&state, &count); err != nil {
				rows.Close()
				return err
			}
			result.ReservationStates[state] = count
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		var reservationCount int64
		if err := tx.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM sales WHERE event_id=$1),(SELECT COUNT(*) FROM reservations WHERE event_id=$1)`, eventID).Scan(&result.ConfirmedSales, &reservationCount); err != nil {
			return err
		}
		if reservationCount > 0 {
			result.ConfirmationRate = float64(result.ConfirmedSales) / float64(reservationCount)
		}
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE state='RECONCILING' AND reconciliation_expires_at<=clock_timestamp()),COUNT(*) FILTER (WHERE (state='HELD' AND hold_expires_at<=clock_timestamp()) OR (state='PAYMENT_RETRY' AND payment_retry_expires_at<=clock_timestamp()) OR (state='RECONCILING' AND reconciliation_expires_at<=clock_timestamp())) FROM reservations WHERE event_id=$1`, eventID).Scan(&result.OverdueReconciliations, &result.DueReservationWork); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `WITH due AS (SELECT hold_expires_at deadline FROM reservations WHERE event_id=$1 AND state='HELD' AND hold_expires_at<=clock_timestamp() UNION ALL SELECT payment_retry_expires_at FROM reservations WHERE event_id=$1 AND state='PAYMENT_RETRY' AND payment_retry_expires_at<=clock_timestamp() UNION ALL SELECT reconciliation_expires_at FROM reservations WHERE event_id=$1 AND state='RECONCILING' AND reconciliation_expires_at<=clock_timestamp()) SELECT COALESCE(EXTRACT(EPOCH FROM clock_timestamp()-MIN(deadline)),0)::bigint FROM due`, eventID).Scan(&result.OldestWorkerLagSeconds); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COUNT(*),COALESCE(EXTRACT(EPOCH FROM clock_timestamp()-MIN(created_at)),0)::bigint FROM outbox_events WHERE event_id=$1 AND processed_at IS NULL`, eventID).Scan(&result.PendingOutbox, &result.OldestOutboxLagSeconds); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE wd.state='PENDING' AND wd.attempt_count>0),COUNT(*) FILTER (WHERE wd.state='DEAD_LETTER') FROM webhook_deliveries wd JOIN outbox_events oe ON oe.id=wd.outbox_event_id WHERE oe.event_id=$1`, eventID).Scan(&result.WebhookFailures, &result.WebhookDeadLetters); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM pg_locks WHERE NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&result.WaitingDatabaseLocks); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT result,COUNT(*) FROM scan_attempts WHERE event_id=$1 GROUP BY result ORDER BY result`, eventID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var outcome string
			var count int64
			if err := rows.Scan(&outcome, &count); err != nil {
				return err
			}
			result.ScanOutcomes[outcome] = count
		}
		return rows.Err()
	})
	if err != nil {
		return OperationalMetrics{}, err
	}
	if result.OverdueReconciliations > 0 {
		result.Alerts = append(result.Alerts, Alert{Code: "RECONCILIATION_OVERDUE", Severity: "WARNING", Message: "Reservations require reconciliation processing", Value: result.OverdueReconciliations})
	}
	if result.OldestOutboxLagSeconds >= 60 {
		result.Alerts = append(result.Alerts, Alert{Code: "OUTBOX_LAG", Severity: "WARNING", Message: "Oldest pending outbox fact is at least 60 seconds old", Value: result.OldestOutboxLagSeconds})
	}
	if result.WebhookDeadLetters > 0 {
		result.Alerts = append(result.Alerts, Alert{Code: "WEBHOOK_DEAD_LETTER", Severity: "CRITICAL", Message: "Webhook deliveries are dead-lettered", Value: result.WebhookDeadLetters})
	}
	if result.WaitingDatabaseLocks > 0 {
		result.Alerts = append(result.Alerts, Alert{Code: "DATABASE_LOCK_CONTENTION", Severity: "WARNING", Message: "Database sessions are waiting on locks", Value: result.WaitingDatabaseLocks})
	}
	if result.Requests.AuthAnomalyCount >= 10 {
		result.Alerts = append(result.Alerts, Alert{Code: "AUTH_ANOMALY_VOLUME", Severity: "WARNING", Message: "Process observed at least 10 authorization failures", Value: int64(result.Requests.AuthAnomalyCount)})
	}
	return result, nil
}
