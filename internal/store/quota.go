package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Default quota values (must match migration defaults).
const (
	DefaultMaxPoolBytes       int64 = 100 * 1024 * 1024 // 100 MiB
	DefaultMaxSingleFileBytes int64 = 10 * 1024 * 1024  // 10 MiB
	DefaultDailyMaxUploads    int   = 10
)

// ErrDailyUploadQuotaExceeded is returned when the user has reached quota_daily_max_uploads for UTC today.
var ErrDailyUploadQuotaExceeded = errors.New("store: daily upload quota exceeded")

// ReserveDailyUpload increments today's upload counter after verifying the user is under the daily cap.
// Call ReleaseDailyUpload if the subsequent upload fails after this succeeds.
func (s *Store) ReserveDailyUpload(ctx context.Context, userID int64) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("store: nil")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var maxDaily int
	if err := tx.QueryRow(ctx, `
		SELECT quota_daily_max_uploads FROM users WHERE id = $1 FOR UPDATE`, userID,
	).Scan(&maxDaily); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgx.ErrNoRows
		}
		return err
	}

	var count int
	err = tx.QueryRow(ctx, `
		SELECT upload_count FROM user_daily_uploads
		WHERE user_id = $1 AND upload_date = (timezone('UTC', now()))::date
		FOR UPDATE`, userID,
	).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		count = 0
	} else if err != nil {
		return err
	}

	if count >= maxDaily {
		return ErrDailyUploadQuotaExceeded
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO user_daily_uploads (user_id, upload_date, upload_count)
		VALUES ($1, (timezone('UTC', now()))::date, 1)
		ON CONFLICT (user_id, upload_date) DO UPDATE
		SET upload_count = user_daily_uploads.upload_count + 1`, userID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReleaseDailyUpload decrements today's counter after a failed upload (best-effort).
func (s *Store) ReleaseDailyUpload(ctx context.Context, userID int64) error {
	if s == nil || s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE user_daily_uploads
		SET upload_count = GREATEST(0, upload_count - 1)
		WHERE user_id = $1 AND upload_date = (timezone('UTC', now()))::date`, userID)
	return err
}

// UpdateUserQuotas sets all three quota columns for a user (full replace).
func (s *Store) UpdateUserQuotas(ctx context.Context, userID int64, maxPool, maxSingle int64, daily int) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("store: nil")
	}
	if err := validateQuotaTriple(maxPool, maxSingle, daily); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET
			quota_max_pool_bytes = $2,
			quota_max_single_file_bytes = $3,
			quota_daily_max_uploads = $4,
			updated_at = now()
		WHERE id = $1`,
		userID, maxPool, maxSingle, daily)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func validateQuotaTriple(maxPool, maxSingle int64, daily int) error {
	if maxPool < 1 || maxSingle < 1 || daily < 1 {
		return fmt.Errorf("quotas must be positive integers")
	}
	if maxSingle > maxPool {
		return fmt.Errorf("max single file cannot exceed pool size")
	}
	const sanityCap int64 = 1 << 40 // 1 TiB
	if maxPool > sanityCap || maxSingle > sanityCap {
		return fmt.Errorf("quota exceeds server sanity cap")
	}
	if daily > 1_000_000 {
		return fmt.Errorf("daily upload cap too large")
	}
	return nil
}
