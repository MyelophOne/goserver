package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type transactionProbe struct {
	pgx.Tx
	commits, rollbacks int
	commitErr          error
	cleanupErr         error
}

func (p *transactionProbe) Commit(context.Context) error { p.commits++; return p.commitErr }
func (p *transactionProbe) Rollback(ctx context.Context) error {
	p.rollbacks++
	p.cleanupErr = ctx.Err()
	return nil
}

func TestFinishTransaction(t *testing.T) {
	success := &transactionProbe{}
	if err := finishTransaction(context.Background(), success, func(pgx.Tx) error { return nil }); err != nil || success.commits != 1 {
		t.Fatalf("successful commit: %v", err)
	}
	sentinel := errors.New("failed")
	for _, failCallback := range []bool{false, true} {
		tx := &transactionProbe{commitErr: sentinel}
		err := finishTransaction(context.Background(), tx, func(pgx.Tx) error {
			if failCallback {
				return sentinel
			}
			return nil
		})
		if !errors.Is(err, sentinel) || tx.rollbacks != 1 {
			t.Fatalf("lost error/cleanup: %v %#v", err, tx)
		}
		if failCallback && tx.commits != 0 {
			t.Fatal("committed failed callback")
		}
		if !failCallback && tx.commits != 1 {
			t.Fatal("missing commit")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &transactionProbe{}
	func() {
		defer func() {
			if recover() != "panic" {
				t.Error("panic swallowed")
			}
		}()
		_ = finishTransaction(ctx, tx, func(pgx.Tx) error { panic("panic") })
	}()
	if tx.commits != 0 || tx.rollbacks != 1 || tx.cleanupErr != nil {
		t.Fatal("panic cleanup inherited canceled context")
	}
	if err := waitRetry(ctx, time.Hour); err != context.Canceled {
		t.Fatal(err)
	}
}
