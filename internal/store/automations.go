package store

import "context"

// AutomationOn is an automation the owner turned on, and where.
type AutomationOn struct {
	Name    string
	Session string
}

// AutomationsOn lists every automation turned on.
func (s *Store) AutomationsOn(ctx context.Context) ([]AutomationOn, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, session FROM automations_on ORDER BY name, session`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AutomationOn
	for rows.Next() {
		var a AutomationOn
		if err := rows.Scan(&a.Name, &a.Session); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetAutomationOn turns an automation on or off there.
func (s *Store) SetAutomationOn(ctx context.Context, name, session string, on bool) error {
	var err error
	if on {
		_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO automations_on (name, session) VALUES (?, ?)`, name, session)
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM automations_on WHERE name = ? AND session = ?`, name, session)
	}
	return err
}

// DropAutomations forgets where an automation, or a session's automations, were on.
func (s *Store) DropAutomations(ctx context.Context, name, session string) error {
	if name != "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM automations_on WHERE name = ?`, name)
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM automations_on WHERE session = ?`, session)
	return err
}
