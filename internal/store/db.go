package store

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"theater-costumes/internal/models"
)

func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path+"?_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}
