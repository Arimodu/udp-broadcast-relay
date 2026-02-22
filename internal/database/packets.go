package database

import (
	"database/sql"
	"fmt"
	"time"
)

func (d *DB) LogPacket(ruleID *int64, srcIP, dstIP string, srcPort, dstPort, size int, direction, clientName string) error {
	_, err := d.db.Exec(
		`INSERT INTO packet_log (rule_id, src_ip, dst_ip, src_port, dst_port, size, direction, client_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ruleID, srcIP, dstIP, srcPort, dstPort, size, direction, clientName,
	)
	return err
}

func (d *DB) GetPacketLog(limit, offset int, ruleID *int64) ([]PacketLogEntry, int, error) {
	// Get total count
	var total int
	var countQuery string
	var countArgs []interface{}
	if ruleID != nil {
		countQuery = `SELECT COUNT(*) FROM packet_log WHERE rule_id = ?`
		countArgs = append(countArgs, *ruleID)
	} else {
		countQuery = `SELECT COUNT(*) FROM packet_log`
	}
	if err := d.db.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Get entries
	var query string
	var args []interface{}
	if ruleID != nil {
		query = `SELECT id, rule_id, src_ip, dst_ip, src_port, dst_port, size, direction, client_name, timestamp FROM packet_log WHERE rule_id = ? ORDER BY timestamp DESC LIMIT ? OFFSET ?`
		args = append(args, *ruleID, limit, offset)
	} else {
		query = `SELECT id, rule_id, src_ip, dst_ip, src_port, dst_port, size, direction, client_name, timestamp FROM packet_log ORDER BY timestamp DESC LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying packet log: %w", err)
	}
	defer rows.Close()

	var entries []PacketLogEntry
	for rows.Next() {
		var e PacketLogEntry
		var ruleIDVal sql.NullInt64
		var clientName sql.NullString
		var timestamp string

		if err := rows.Scan(&e.ID, &ruleIDVal, &e.SrcIP, &e.DstIP, &e.SrcPort, &e.DstPort, &e.Size, &e.Direction, &clientName, &timestamp); err != nil {
			return nil, 0, fmt.Errorf("scanning packet log: %w", err)
		}

		if ruleIDVal.Valid {
			e.RuleID = &ruleIDVal.Int64
		}
		if clientName.Valid {
			e.ClientName = clientName.String
		}
		e.Timestamp, _ = time.Parse("2006-01-02 15:04:05", timestamp)
		entries = append(entries, e)
	}

	return entries, total, rows.Err()
}

func (d *DB) PrunePacketLog(maxEntries int) error {
	_, err := d.db.Exec(
		`DELETE FROM packet_log WHERE id NOT IN (SELECT id FROM packet_log ORDER BY timestamp DESC LIMIT ?)`,
		maxEntries,
	)
	return err
}

// Broadcast observations
func (d *DB) UpsertBroadcastObservation(srcIP, dstIP string, srcPort, dstPort int, protocolType string) error {
	_, err := d.db.Exec(
		`INSERT INTO broadcast_observations (src_ip, dst_ip, src_port, dst_port, protocol_type)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(src_ip, dst_ip, src_port, dst_port) DO UPDATE SET
		   last_seen = datetime('now'),
		   count = count + 1,
		   protocol_type = excluded.protocol_type`,
		srcIP, dstIP, srcPort, dstPort, protocolType,
	)
	return err
}

func (d *DB) GetBroadcastObservations() ([]BroadcastObservation, error) {
	rows, err := d.db.Query(
		`SELECT id, src_ip, dst_ip, src_port, dst_port, protocol_type, first_seen, last_seen, count, has_rule
		 FROM broadcast_observations ORDER BY last_seen DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying broadcast observations: %w", err)
	}
	defer rows.Close()

	var observations []BroadcastObservation
	for rows.Next() {
		var o BroadcastObservation
		var hasRule int
		var firstSeen, lastSeen string

		if err := rows.Scan(&o.ID, &o.SrcIP, &o.DstIP, &o.SrcPort, &o.DstPort, &o.ProtocolType, &firstSeen, &lastSeen, &o.Count, &hasRule); err != nil {
			return nil, fmt.Errorf("scanning observation: %w", err)
		}

		o.HasRule = hasRule == 1
		o.FirstSeen, _ = time.Parse("2006-01-02 15:04:05", firstSeen)
		o.LastSeen, _ = time.Parse("2006-01-02 15:04:05", lastSeen)
		observations = append(observations, o)
	}
	return observations, rows.Err()
}

func (d *DB) UpdateObservationHasRule(srcPort, dstPort int, hasRule bool) error {
	val := 0
	if hasRule {
		val = 1
	}
	_, err := d.db.Exec(
		`UPDATE broadcast_observations SET has_rule = ? WHERE dst_port = ?`,
		val, dstPort,
	)
	return err
}
