// Health-check handlers.
//
// Two endpoints:
//   - GET /health        : liveness probe (always 200 if the process is up).
//   - GET /health/ready  : readiness probe that pings the database.
//
// In Kubernetes/Cloud Run terms: /health is the liveness probe (the
// process can serve requests at all), /health/ready is the readiness probe
// (the process can serve requests *correctly*, i.e. the DB is reachable).
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthResponse is the body returned by GET /health and GET /health/ready.
type HealthResponse struct {
	Status    string `json:"status"`    // "ok" | "degraded"
	Version   string `json:"version"`
	Time      string `json:"time"`      // RFC3339
	DB        string `json:"db"`        // "ok" | "unreachable" | "skipped"
	RequestID string `json:"request_id,omitempty"`
}

// Health returns a liveness handler. It does NOT touch the database so it
// can respond even when the DB is down (Kubernetes should not restart a
// pod just because the DB is unreachable).
func Health() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, HealthResponse{
			Status:  "ok",
			Version: APIVersion,
			Time:    time.Now().UTC().Format(time.RFC3339),
			DB:      "skipped",
		})
	}
}

// HealthReady returns a readiness handler. It pings the database with a
// 2-second timeout and reports degraded status if the ping fails. The HTTP
// status is 200 when the DB is reachable, 503 when it is not — this lets
// the load balancer drain traffic from the instance.
func HealthReady(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "ok"
		httpStatus := http.StatusOK

		if pool == nil {
			dbStatus = "unreachable"
			httpStatus = http.StatusServiceUnavailable
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := pool.Ping(ctx); err != nil {
				dbStatus = "unreachable"
				httpStatus = http.StatusServiceUnavailable
			}
		}

		overall := "ok"
		if httpStatus != http.StatusOK {
			overall = "degraded"
		}

		respondJSON(w, httpStatus, HealthResponse{
			Status:  overall,
			Version: APIVersion,
			Time:    time.Now().UTC().Format(time.RFC3339),
			DB:      dbStatus,
		})
	}
}
