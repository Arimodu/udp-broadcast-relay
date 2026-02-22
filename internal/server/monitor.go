package server

import (
	"log/slog"

	"github.com/Arimodu/udp-broadcast-relay/internal/database"
)

type Monitor struct {
	ifaceName string
	db        *database.DB
	log       *slog.Logger
}

func NewMonitor(ifaceName string, db *database.DB, log *slog.Logger) *Monitor {
	return &Monitor{
		ifaceName: ifaceName,
		db:        db,
		log:       log,
	}
}
