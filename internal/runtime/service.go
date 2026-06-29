package runtime

import (
	"context"
	"fmt"

	"github.com/appliedsymbolics/sigint/internal/config"
	"github.com/appliedsymbolics/sigint/internal/ingest"
	"github.com/appliedsymbolics/sigint/internal/ledger"
	"github.com/appliedsymbolics/sigint/internal/storage"
)

type ServiceHandle struct {
	Service *ingest.Service
	close   func() error
}

func NewServiceFromConfig(ctx context.Context, configPath string) (*ServiceHandle, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	return NewService(ctx, cfg)
}

func NewService(ctx context.Context, cfg config.App) (*ServiceHandle, error) {
	eventLedger, closeLedger, err := newLedger(ctx, cfg.Ledger)
	if err != nil {
		return nil, err
	}

	eventStorage, err := newStorage(ctx, cfg.Storage)
	if err != nil {
		_ = closeLedger()
		return nil, err
	}

	service := ingest.NewService(eventLedger, eventStorage, cfg.Ingest)
	if err := service.Initialize(ctx); err != nil {
		_ = closeLedger()
		return nil, fmt.Errorf("initialize ledger: %w", err)
	}

	return &ServiceHandle{
		Service: service,
		close:   closeLedger,
	}, nil
}

func InitializeLedger(ctx context.Context, cfg config.Ledger) error {
	eventLedger, closeLedger, err := newLedger(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = closeLedger() }()
	if err := eventLedger.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize ledger: %w", err)
	}
	return nil
}

func (h *ServiceHandle) Close() error {
	if h == nil || h.close == nil {
		return nil
	}
	return h.close()
}

func newLedger(ctx context.Context, cfg config.Ledger) (ingest.Ledger, func() error, error) {
	switch cfg.Adapter {
	case "sqlite":
		db, err := ledger.NewSQLite(cfg.Path)
		if err != nil {
			return nil, nil, err
		}
		return db, db.Close, nil
	case "postgres":
		db, err := ledger.NewPostgres(ctx, cfg.DSN)
		if err != nil {
			return nil, nil, err
		}
		return db, func() error {
			db.Close()
			return nil
		}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported ledger adapter %q; supported adapters are sqlite and postgres", cfg.Adapter)
	}
}

func newStorage(ctx context.Context, cfg config.Storage) (ingest.Storage, error) {
	switch cfg.Adapter {
	case "filesystem":
		return storage.NewFilesystem(cfg.Root), nil
	case "s3":
		return storage.NewS3(ctx, storage.S3Options{
			Bucket:               cfg.Bucket,
			Prefix:               cfg.Prefix,
			Region:               cfg.Region,
			Endpoint:             cfg.Endpoint,
			ForcePathStyle:       cfg.ForcePathStyle,
			ServerSideEncryption: cfg.ServerSideEncryption,
			KMSKeyID:             cfg.KMSKeyID,
		})
	default:
		return nil, fmt.Errorf("unsupported storage adapter %q; supported adapters are filesystem and s3", cfg.Adapter)
	}
}
