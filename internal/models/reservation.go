package models

import (
	"fmt"
	"time"
)

// ReservationStatus represents the status of a reservation.
type ReservationStatus string

const (
	ReservationPending   ReservationStatus = "pending"
	ReservationActive    ReservationStatus = "active"
	ReservationReleased  ReservationStatus = "released"
	ReservationExpired   ReservationStatus = "expired"
	ReservationCancelled ReservationStatus = "cancelled"
)

// Reservation represents a soft reservation for quota usage.
type Reservation struct {
	ID               string            `json:"id"`
	AccountID        string            `json:"account_id"`
	EstimatedCostPct float64           `json:"estimated_cost_percent"`
	Status           ReservationStatus `json:"status"`
	CreatedAt        time.Time         `json:"created_at"`
	ExpiresAt        time.Time         `json:"expires_at"`
	ReleasedAt       *time.Time        `json:"released_at,omitempty"`
	CorrelationID    string            `json:"correlation_id"`
	ActualCostPct    *float64          `json:"actual_cost_percent,omitempty"`
}

// Validate checks if the reservation is valid.
func (r *Reservation) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("reservation ID is required")
	}
	if r.AccountID == "" {
		return fmt.Errorf("account ID is required")
	}
	if r.EstimatedCostPct < 0 {
		return fmt.Errorf("estimated cost cannot be negative")
	}
	if r.EstimatedCostPct > 100 {
		return fmt.Errorf("estimated cost cannot exceed 100")
	}
	if r.Status == "" {
		return fmt.Errorf("status is required")
	}
	if r.CorrelationID == "" {
		return fmt.Errorf("correlation ID is required")
	}
	return nil
}

// IsExpired returns true if the reservation has expired.
func (r *Reservation) IsExpired() bool {
	return time.Now().After(r.ExpiresAt)
}

// IsActive returns true if the reservation is active.
func (r *Reservation) IsActive() bool {
	return r.Status == ReservationActive && !r.IsExpired()
}

// CanRelease returns true if the reservation can be released.
func (r *Reservation) CanRelease() bool {
	return r.Status == ReservationActive || r.Status == ReservationPending
}

// Release marks the reservation as released.
func (r *Reservation) Release(actualCostPct float64) error {
	if !r.CanRelease() {
		return fmt.Errorf("cannot release reservation with status %s", r.Status)
	}
	now := time.Now()
	r.ReleasedAt = &now
	r.ActualCostPct = &actualCostPct
	r.Status = ReservationReleased
	return nil
}

// Cancel marks the reservation as cancelled.
func (r *Reservation) Cancel() error {
	if !r.CanRelease() {
		return fmt.Errorf("cannot cancel reservation with status %s", r.Status)
	}
	r.Status = ReservationCancelled
	return nil
}

// Expire marks the reservation as expired.
func (r *Reservation) Expire() error {
	if r.Status != ReservationActive && r.Status != ReservationPending {
		return fmt.Errorf("cannot expire reservation with status %s", r.Status)
	}
	r.Status = ReservationExpired
	return nil
}

// TimeUntilExpiry returns the duration until expiry.
func (r *Reservation) TimeUntilExpiry() time.Duration {
	until := time.Until(r.ExpiresAt)
	if until < 0 {
		return 0
	}
	return until
}

// Age returns the age of the reservation.
func (r *Reservation) Age() time.Duration {
	return time.Since(r.CreatedAt)
}

// ReservationSlice is a slice of reservations with helper methods.
type ReservationSlice []Reservation

// FilterActive returns only active reservations.
func (rs ReservationSlice) FilterActive() ReservationSlice {
	var result ReservationSlice
	for _, r := range rs {
		if r.IsActive() {
			result = append(result, r)
		}
	}
	return result
}

// FilterByAccountID returns reservations for a specific account.
func (rs ReservationSlice) FilterByAccountID(accountID string) ReservationSlice {
	var result ReservationSlice
	for _, r := range rs {
		if r.AccountID == accountID {
			result = append(result, r)
		}
	}
	return result
}

// TotalEstimatedCost returns the sum of estimated costs for all reservations.
func (rs ReservationSlice) TotalEstimatedCost() float64 {
	var total float64
	for _, r := range rs {
		if r.IsActive() {
			total += r.EstimatedCostPct
		}
	}
	return total
}

// FindByID returns a reservation by ID.
func (rs ReservationSlice) FindByID(id string) (*Reservation, bool) {
	for i := range rs {
		if rs[i].ID == id {
			return &rs[i], true
		}
	}
	return nil, false
}
