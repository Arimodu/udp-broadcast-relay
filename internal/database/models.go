package database

import "time"

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	IsAdmin      bool
	IsActive     bool
	CreatedAt    time.Time
	LastLoginAt  *time.Time
}

type APIKey struct {
	ID         int64
	UserID     int64
	Key        string
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	IsRevoked  bool
}

type Session struct {
	ID        int64
	UserID    int64
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type ForwardRule struct {
	ID            int64
	Name          string
	ListenPort    int
	ListenIP      string
	DestBroadcast string
	Direction     string // "server_to_client", "client_to_server", "bidirectional"
	IsEnabled     bool
	CreatedAt     time.Time
}

type ClientRule struct {
	ClientKeyID int64
	RuleID      int64
}

type PacketLogEntry struct {
	ID         int64
	RuleID     *int64
	SrcIP      string
	DstIP      string
	SrcPort    int
	DstPort    int
	Size       int
	Direction  string
	ClientName string
	Timestamp  time.Time
}

type BroadcastObservation struct {
	ID           int64     `json:"id"`
	SrcIP        string    `json:"src_ip"`
	DstIP        string    `json:"dst_ip"`
	SrcPort      int       `json:"src_port"`
	DstPort      int       `json:"dst_port"`
	ProtocolType string    `json:"protocol_type"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
	Count        int64     `json:"count"`
	HasRule      bool      `json:"has_rule"`
}
