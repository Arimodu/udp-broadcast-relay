package database

import (
	"database/sql"
	"fmt"
	"time"
)

func (d *DB) CreateRule(name string, listenPort int, listenIP, destBroadcast, direction string) (*ForwardRule, error) {
	result, err := d.db.Exec(
		`INSERT INTO forward_rules (name, listen_port, listen_ip, dest_broadcast, direction) VALUES (?, ?, ?, ?, ?)`,
		name, listenPort, listenIP, destBroadcast, direction,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting rule: %w", err)
	}

	id, _ := result.LastInsertId()
	return &ForwardRule{
		ID:            id,
		Name:          name,
		ListenPort:    listenPort,
		ListenIP:      listenIP,
		DestBroadcast: destBroadcast,
		Direction:     direction,
		IsEnabled:     true,
		CreatedAt:     time.Now(),
	}, nil
}

func (d *DB) GetRule(id int64) (*ForwardRule, error) {
	r := &ForwardRule{}
	var isEnabled int
	var createdAt string

	err := d.db.QueryRow(
		`SELECT id, name, listen_port, listen_ip, dest_broadcast, direction, is_enabled, created_at FROM forward_rules WHERE id = ?`,
		id,
	).Scan(&r.ID, &r.Name, &r.ListenPort, &r.ListenIP, &r.DestBroadcast, &r.Direction, &isEnabled, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying rule: %w", err)
	}

	r.IsEnabled = isEnabled == 1
	r.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return r, nil
}

func (d *DB) ListRules() ([]ForwardRule, error) {
	rows, err := d.db.Query(
		`SELECT id, name, listen_port, listen_ip, dest_broadcast, direction, is_enabled, created_at FROM forward_rules ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying rules: %w", err)
	}
	defer rows.Close()

	return scanRules(rows)
}

func (d *DB) ListEnabledRules() ([]ForwardRule, error) {
	rows, err := d.db.Query(
		`SELECT id, name, listen_port, listen_ip, dest_broadcast, direction, is_enabled, created_at FROM forward_rules WHERE is_enabled = 1 ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying enabled rules: %w", err)
	}
	defer rows.Close()

	return scanRules(rows)
}

func scanRules(rows *sql.Rows) ([]ForwardRule, error) {
	rules := make([]ForwardRule, 0)
	for rows.Next() {
		var r ForwardRule
		var isEnabled int
		var createdAt string

		if err := rows.Scan(&r.ID, &r.Name, &r.ListenPort, &r.ListenIP, &r.DestBroadcast, &r.Direction, &isEnabled, &createdAt); err != nil {
			return nil, fmt.Errorf("scanning rule: %w", err)
		}

		r.IsEnabled = isEnabled == 1
		r.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (d *DB) UpdateRule(id int64, name string, listenPort int, listenIP, destBroadcast, direction string) error {
	_, err := d.db.Exec(
		`UPDATE forward_rules SET name = ?, listen_port = ?, listen_ip = ?, dest_broadcast = ?, direction = ? WHERE id = ?`,
		name, listenPort, listenIP, destBroadcast, direction, id,
	)
	return err
}

func (d *DB) ToggleRule(id int64) error {
	_, err := d.db.Exec(
		`UPDATE forward_rules SET is_enabled = CASE WHEN is_enabled = 1 THEN 0 ELSE 1 END WHERE id = ?`, id,
	)
	return err
}

func (d *DB) DeleteRule(id int64) error {
	_, err := d.db.Exec(`DELETE FROM forward_rules WHERE id = ?`, id)
	return err
}

// Client-rule assignments
func (d *DB) AssignRuleToClient(keyID, ruleID int64) error {
	_, err := d.db.Exec(
		`INSERT OR IGNORE INTO client_rules (client_key_id, rule_id) VALUES (?, ?)`,
		keyID, ruleID,
	)
	return err
}

func (d *DB) UnassignRuleFromClient(keyID, ruleID int64) error {
	_, err := d.db.Exec(
		`DELETE FROM client_rules WHERE client_key_id = ? AND rule_id = ?`,
		keyID, ruleID,
	)
	return err
}

func (d *DB) GetRulesForClient(keyID int64) ([]ForwardRule, error) {
	rows, err := d.db.Query(
		`SELECT r.id, r.name, r.listen_port, r.listen_ip, r.dest_broadcast, r.direction, r.is_enabled, r.created_at
		 FROM forward_rules r
		 INNER JOIN client_rules cr ON cr.rule_id = r.id
		 WHERE cr.client_key_id = ? AND r.is_enabled = 1
		 ORDER BY r.id`,
		keyID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying client rules: %w", err)
	}
	defer rows.Close()

	return scanRules(rows)
}

// EnabledRuleCountForPort returns how many enabled rules listen on the given port.
// Used to decide whether broadcast_observations.has_rule should be true or false.
func (d *DB) EnabledRuleCountForPort(port int) (int, error) {
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM forward_rules WHERE listen_port = ? AND is_enabled = 1`, port,
	).Scan(&count)
	return count, err
}

func (d *DB) GetClientKeysForRule(ruleID int64) ([]int64, error) {
	rows, err := d.db.Query(
		`SELECT client_key_id FROM client_rules WHERE rule_id = ?`, ruleID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
