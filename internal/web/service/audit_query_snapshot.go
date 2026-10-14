package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
)

var auditQuerySlots = make(chan struct{}, 2)

// auditReadSnapshot keeps all dashboard metrics in one consistent phase while
// allowing ingestion to proceed. Bound analytical readers to leave DB capacity
// available for writers and node RPCs.
func auditReadSnapshot(read func(*gorm.DB) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	select {
	case auditQuerySlots <- struct{}{}:
		defer func() { <-auditQuerySlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	db := database.GetDB().WithContext(ctx)
	if database.IsPostgres() {
		return db.Transaction(read, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	}
	// The normal SQLite DSN uses _txlock=immediate for writes. Use a dedicated
	// connection and explicit deferred BEGIN for this read-only WAL snapshot.
	return db.Connection(func(tx *gorm.DB) (err error) {
		tx = tx.Session(&gorm.Session{NewDB: true})
		if err = tx.Exec("BEGIN DEFERRED").Error; err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				rollback := tx.Session(&gorm.Session{Context: context.Background()}).Exec("ROLLBACK").Error
				err = errors.Join(err, rollback)
			}
		}()
		if err = read(tx); err != nil {
			return err
		}
		err = tx.Exec("COMMIT").Error
		committed = err == nil
		return err
	})
}
