package bootstrap

import (
	"fmt"

	"github.com/udaypankhaniya/trangly/internal/app"
	"github.com/udaypankhaniya/trangly/internal/config"
	"github.com/udaypankhaniya/trangly/internal/infra/db"
)

// SetupAdmin opens the database, derives the master key (creating it if absent),
// and creates the admin account. It is intentionally minimal — it does NOT start
// Docker, the scheduler, or the HTTP server. Safe to run before the first serve.
func SetupAdmin(cfg config.Config, username, email, password string) error {
	database, err := db.New(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("setup: open database: %w", err)
	}

	masterKey, err := loadOrCreateMasterKey(cfg.MasterKeyPath)
	if err != nil {
		return fmt.Errorf("setup: master key: %w", err)
	}

	authSvc, err := app.NewAuthService(database, masterKey)
	if err != nil {
		return fmt.Errorf("setup: auth service: %w", err)
	}

	if err := authSvc.CreateAdmin(username, email, password); err != nil {
		return fmt.Errorf("setup: %w", err)
	}

	return nil
}
