package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

const locationCols = `id, user_id, name, region, country, country_code, lat, lon, elevation, timezone, is_home, warn, sort, note, source, source_ref, created_at, updated_at`

func scanLocation(sc interface{ Scan(...any) error }) (*Location, error) {
	var l Location
	var elev sql.NullFloat64
	var home, warn int
	if err := sc.Scan(&l.ID, &l.UserID, &l.Name, &l.Region, &l.Country, &l.CountryCode, &l.Lat, &l.Lon, &elev, &l.Timezone, &home, &warn, &l.Sort, &l.Note,
		&l.Source, &l.SourceRef, &l.CreatedAt, &l.UpdatedAt); err != nil {
		return nil, err
	}
	if elev.Valid {
		v := elev.Float64
		l.Elevation = &v
	}
	l.IsHome, l.Warn = home == 1, warn == 1
	return &l, nil
}

// ListLocations returns one person's places, home first, then in their order.
func (s *Store) ListLocations(ctx context.Context, uid int64) ([]Location, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+locationCols+` FROM locations WHERE user_id = ? ORDER BY is_home DESC, source = 'here' DESC, sort, name COLLATE NOCASE`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Location{}
	for rows.Next() {
		l, err := scanLocation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// GetLocation returns one of the person's places (ErrNotFound for anyone else's).
func (s *Store) GetLocation(ctx context.Context, uid, id int64) (*Location, error) {
	l, err := scanLocation(s.DB.QueryRowContext(ctx, `SELECT `+locationCols+` FROM locations WHERE user_id = ? AND id = ?`, uid, id))
	return l, notFound(err)
}

// HomeLocation is the person's home place (else their first one).
func (s *Store) HomeLocation(ctx context.Context, uid int64) (*Location, error) {
	l, err := scanLocation(s.DB.QueryRowContext(ctx, `SELECT `+locationCols+` FROM locations WHERE user_id = ? AND source <> 'here' ORDER BY is_home DESC, sort, id LIMIT 1`, uid))
	if err == nil {
		return l, nil
	}
	l, err = scanLocation(s.DB.QueryRowContext(ctx, `SELECT `+locationCols+` FROM locations WHERE user_id = ? ORDER BY id LIMIT 1`, uid))
	return l, notFound(err)
}

// LocationBySource finds a place another app (or "here") added.
func (s *Store) LocationBySource(ctx context.Context, uid int64, source, ref string) (*Location, error) {
	l, err := scanLocation(s.DB.QueryRowContext(ctx, `SELECT `+locationCols+` FROM locations WHERE user_id = ? AND source = ? AND source_ref = ?`, uid, source, ref))
	return l, notFound(err)
}

// CreateLocation adds a place; the first one becomes home.
func (s *Store) CreateLocation(ctx context.Context, l *Location) error {
	l.Name = strings.TrimSpace(l.Name)
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM locations WHERE user_id = ? AND source <> 'here'`, l.UserID).Scan(&n)
	if n == 0 && l.Source != "here" {
		l.IsHome = true
	}
	var maxSort int
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort), 0) FROM locations WHERE user_id = ?`, l.UserID).Scan(&maxSort)
	if l.Sort == 0 {
		l.Sort = maxSort + 1
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if l.IsHome {
		if _, err := tx.ExecContext(ctx, `UPDATE locations SET is_home = 0 WHERE user_id = ?`, l.UserID); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO locations (user_id, name, region, country, country_code, lat, lon, elevation, timezone, is_home, warn, sort, note, source, source_ref)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.UserID, l.Name, l.Region, l.Country, l.CountryCode, l.Lat, l.Lon, nullF(l.Elevation), l.Timezone, boolInt(l.IsHome), boolInt(l.Warn), l.Sort, l.Note, l.Source, l.SourceRef)
	if err != nil {
		return conflict(err)
	}
	l.ID, _ = res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return err
	}
	got, err := s.GetLocation(ctx, l.UserID, l.ID)
	if err == nil {
		*l = *got
	}
	return err
}

// UpdateLocation saves a place; making it home takes home from the others.
func (s *Store) UpdateLocation(ctx context.Context, l *Location) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if l.IsHome {
		if _, err := tx.ExecContext(ctx, `UPDATE locations SET is_home = 0 WHERE user_id = ? AND id <> ?`, l.UserID, l.ID); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE locations SET name = ?, region = ?, country = ?, country_code = ?, lat = ?, lon = ?, elevation = ?, timezone = ?, is_home = ?, warn = ?, sort = ?, note = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND id = ?`,
		strings.TrimSpace(l.Name), l.Region, l.Country, l.CountryCode, l.Lat, l.Lon, nullF(l.Elevation), l.Timezone, boolInt(l.IsHome), boolInt(l.Warn), l.Sort, l.Note, l.UserID, l.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// DeleteLocation removes a place with its diary and warnings; when it was
// home, the next place becomes home.
func (s *Store) DeleteLocation(ctx context.Context, uid, id int64) error {
	l, err := s.GetLocation(ctx, uid, id)
	if err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM locations WHERE user_id = ? AND id = ?`, uid, id); err != nil {
		return err
	}
	if l.IsHome {
		_, _ = s.DB.ExecContext(ctx, `UPDATE locations SET is_home = 1 WHERE id = (SELECT id FROM locations WHERE user_id = ? AND source <> 'here' ORDER BY sort, id LIMIT 1)`, uid)
	}
	return nil
}

// ReorderLocations sets the order of the person's places (ids not theirs are skipped).
func (s *Store) ReorderLocations(ctx context.Context, uid int64, ids []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE locations SET sort = ? WHERE user_id = ? AND id = ?`, i+1, uid, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LocationUsers lists everyone who follows at least one place.
func (s *Store) LocationUsers(ctx context.Context) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT user_id FROM locations ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func nullF(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func optF(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

// ---- the weather diary ----

const dayCols = `location_id, date, code, t_min, t_max, precip, snow, wind, gust, uv, sunshine_h, aqi, pollen, note, updated_at`

func scanDay(sc interface{ Scan(...any) error }) (*DayRecord, error) {
	var d DayRecord
	var tmin, tmax, precip, snow, wind, gust, uv, sun, aqi sql.NullFloat64
	var pollen string
	if err := sc.Scan(&d.LocationID, &d.Date, &d.Code, &tmin, &tmax, &precip, &snow, &wind, &gust, &uv, &sun, &aqi, &pollen, &d.Note, &d.UpdatedAt); err != nil {
		return nil, err
	}
	d.TMin, d.TMax, d.Precip, d.Snow, d.Wind, d.Gust, d.UV, d.SunshineH, d.AQI = optF(tmin), optF(tmax), optF(precip), optF(snow), optF(wind), optF(gust), optF(uv), optF(sun), optF(aqi)
	d.Pollen = map[string]float64{}
	_ = json.Unmarshal([]byte(pollen), &d.Pollen)
	return &d, nil
}

// SaveDay writes a diary day (the person's note is kept).
func (s *Store) SaveDay(ctx context.Context, uid int64, d DayRecord) error {
	if d.Pollen == nil {
		d.Pollen = map[string]float64{}
	}
	pollen, _ := json.Marshal(d.Pollen)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO days (user_id, location_id, date, code, t_min, t_max, precip, snow, wind, gust, uv, sunshine_h, aqi, pollen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, location_id, date) DO UPDATE SET code = excluded.code, t_min = excluded.t_min, t_max = excluded.t_max, precip = excluded.precip,
		snow = excluded.snow, wind = excluded.wind, gust = excluded.gust, uv = COALESCE(excluded.uv, days.uv), sunshine_h = excluded.sunshine_h,
		aqi = COALESCE(excluded.aqi, days.aqi), pollen = CASE WHEN excluded.pollen = '{}' THEN days.pollen ELSE excluded.pollen END,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		uid, d.LocationID, d.Date, d.Code, nullF(d.TMin), nullF(d.TMax), nullF(d.Precip), nullF(d.Snow), nullF(d.Wind), nullF(d.Gust), nullF(d.UV), nullF(d.SunshineH), nullF(d.AQI), string(pollen))
	return err
}

// SetDayNote saves the person's own words for a day (the day must be in the diary).
func (s *Store) SetDayNote(ctx context.Context, uid, loc int64, date, note string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE days SET note = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND location_id = ? AND date = ?`, strings.TrimSpace(note), uid, loc, date)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetDay returns one diary day.
func (s *Store) GetDay(ctx context.Context, uid, loc int64, date string) (*DayRecord, error) {
	d, err := scanDay(s.DB.QueryRowContext(ctx, `SELECT `+dayCols+` FROM days WHERE user_id = ? AND location_id = ? AND date = ?`, uid, loc, date))
	return d, notFound(err)
}

// ListDays returns a place's diary from..to (inclusive), newest first.
func (s *Store) ListDays(ctx context.Context, uid, loc int64, from, to string) ([]DayRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+dayCols+` FROM days WHERE user_id = ? AND location_id = ? AND date >= ? AND date <= ? ORDER BY date DESC`, uid, loc, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DayRecord{}
	for rows.Next() {
		d, err := scanDay(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// AllDays is the whole diary of a person (for the export).
func (s *Store) AllDays(ctx context.Context, uid int64) ([]DayRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+dayCols+` FROM days WHERE user_id = ? ORDER BY location_id, date`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DayRecord{}
	for rows.Next() {
		d, err := scanDay(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// ---- warnings ----

const warningCols = `w.id, w.location_id, w.kind, w.level, w.day, w.starts_at, w.ends_at, w.title, w.detail, w.value, w.notified, w.gone, w.created_at, w.updated_at, l.name`

func scanWarning(sc interface{ Scan(...any) error }) (*Warning, error) {
	var w Warning
	var v sql.NullFloat64
	var gone int
	if err := sc.Scan(&w.ID, &w.LocationID, &w.Kind, &w.Level, &w.Day, &w.StartsAt, &w.EndsAt, &w.Title, &w.Detail, &v, &w.Notified, &gone, &w.CreatedAt, &w.UpdatedAt, &w.Place); err != nil {
		return nil, err
	}
	w.Value, w.Gone = optF(v), gone == 1
	return &w, nil
}

// UpsertWarning saves what the forecast shows now. It returns the stored
// warning and whether it is new or got worse (advisory → warning), which is
// when the person hears about it.
func (s *Store) UpsertWarning(ctx context.Context, uid int64, w Warning) (*Warning, bool, error) {
	var oldLevel string
	var oldGone int
	err := s.DB.QueryRowContext(ctx, `SELECT level, gone FROM warnings WHERE user_id = ? AND location_id = ? AND kind = ? AND day = ?`, uid, w.LocationID, w.Kind, w.Day).Scan(&oldLevel, &oldGone)
	fresh := err == sql.ErrNoRows || oldGone == 1 || (oldLevel == "advisory" && w.Level == "warning")
	if err != nil && err != sql.ErrNoRows {
		return nil, false, err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO warnings (user_id, location_id, kind, level, day, starts_at, ends_at, title, detail, value)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, location_id, kind, day) DO UPDATE SET level = excluded.level, starts_at = excluded.starts_at, ends_at = excluded.ends_at,
		title = excluded.title, detail = excluded.detail, value = excluded.value, gone = 0, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		uid, w.LocationID, w.Kind, w.Level, w.Day, w.StartsAt, w.EndsAt, w.Title, w.Detail, nullF(w.Value))
	if err != nil {
		return nil, false, err
	}
	got, err := scanWarning(s.DB.QueryRowContext(ctx, `SELECT `+warningCols+` FROM warnings w JOIN locations l ON l.id = w.location_id
		WHERE w.user_id = ? AND w.location_id = ? AND w.kind = ? AND w.day = ?`, uid, w.LocationID, w.Kind, w.Day))
	return got, fresh, err
}

// SetWarningNotified records the level last pushed.
func (s *Store) SetWarningNotified(ctx context.Context, uid, id int64, level string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE warnings SET notified = ? WHERE user_id = ? AND id = ?`, level, uid, id)
	return err
}

// ClearWarnings marks the warnings of a place from a day on that the
// forecast no longer shows (keep = the ids it still shows).
func (s *Store) ClearWarnings(ctx context.Context, uid, loc int64, fromDay string, keep []int64) error {
	q := `UPDATE warnings SET gone = 1, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND location_id = ? AND day >= ? AND gone = 0`
	args := []any{uid, loc, fromDay}
	if len(keep) > 0 {
		q += ` AND id NOT IN (` + strings.TrimSuffix(strings.Repeat("?,", len(keep)), ",") + `)`
		for _, id := range keep {
			args = append(args, id)
		}
	}
	_, err := s.DB.ExecContext(ctx, q, args...)
	return err
}

// ListWarnings returns the person's warnings from a day on (active = not gone), soonest first.
func (s *Store) ListWarnings(ctx context.Context, uid int64, fromDay string, active bool, loc int64) ([]Warning, error) {
	q := `SELECT ` + warningCols + ` FROM warnings w JOIN locations l ON l.id = w.location_id WHERE w.user_id = ? AND w.day >= ?`
	args := []any{uid, fromDay}
	if active {
		q += ` AND w.gone = 0`
	}
	if loc > 0 {
		q += ` AND w.location_id = ?`
		args = append(args, loc)
	}
	q += ` ORDER BY w.day, CASE w.level WHEN 'warning' THEN 0 ELSE 1 END, w.starts_at, w.kind`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Warning{}
	for rows.Next() {
		w, err := scanWarning(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// RecentWarnings returns warnings between two days, newest first (the history).
func (s *Store) RecentWarnings(ctx context.Context, uid int64, from, to string) ([]Warning, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+warningCols+` FROM warnings w JOIN locations l ON l.id = w.location_id
		WHERE w.user_id = ? AND w.day >= ? AND w.day <= ? AND w.gone = 0 ORDER BY w.day DESC, w.starts_at DESC`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Warning{}
	for rows.Next() {
		w, err := scanWarning(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// ---- what was done with advice ----

// Marks returns the person's advice marks by key.
func (s *Store) Marks(ctx context.Context, uid int64) (map[string]Mark, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT key, state, ref, created_at FROM advice_marks WHERE user_id = ?`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Mark{}
	for rows.Next() {
		var m Mark
		if err := rows.Scan(&m.Key, &m.State, &m.Ref, &m.CreatedAt); err != nil {
			return nil, err
		}
		out[m.Key] = m
	}
	return out, rows.Err()
}

// SetMark records what the person did with a piece of advice ("" state = forget it).
func (s *Store) SetMark(ctx context.Context, uid int64, key, state, ref string) error {
	if state == "" {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM advice_marks WHERE user_id = ? AND key = ?`, uid, key)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO advice_marks (user_id, key, state, ref) VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET state = excluded.state, ref = excluded.ref, created_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`, uid, key, state, ref)
	return err
}

// ListMarks returns every mark (for the export).
func (s *Store) ListMarks(ctx context.Context, uid int64) ([]Mark, error) {
	m, err := s.Marks(ctx, uid)
	out := make([]Mark, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out, err
}

// CachePurge drops cached entries with the prefix fetched before a time (RFC 3339).
func (s *Store) CachePurge(ctx context.Context, prefix, before string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM cache WHERE key LIKE ? || '%' AND fetched_at < ?`, prefix, before)
	return err
}
