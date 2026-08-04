package store

import "database/sql"

// RepriceUsage recomputes stored cost from the stored token buckets and the rate
// table as it looks right now, for every usage row ATM has.
//
// It exists because cost is written once, at insert time, and never revisited:
// usage_events rows are inserted with INSERT OR IGNORE against the fingerprint
// index, so re-parsing a transcript is a no-op and a request keeps whatever the
// table said the first time ATM saw it. Without this pass, any rate change — a
// model added to the built-in table, a family fallback that did not exist
// before, or an entry the user put in ~/.atm/pricing.json — would apply only to
// requests ATM had not indexed yet, leaving historical spend frozen at the old
// guess. That is how a table covering nine models left months of GPT and GLM
// traffic priced at the Opus default.
//
// Sync calls this, so editing ~/.atm/pricing.json is enough: the next sync moves
// history onto the new rate. The pass is grouped by model rather than by row, so
// its cost is one statement per distinct model, not one per request.
func RepriceUsage(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := repriceUsage(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func repriceUsage(tx *sql.Tx) error {
	// usage_events first: the usage rollup is rebuilt from it below.
	models, err := distinctUsageModels(tx, "usage_events")
	if err != nil {
		return err
	}
	for _, model := range models {
		p, _ := lookupPricing(model)
		if err := execTx(tx, `UPDATE usage_events SET cost_usd =
			input_tokens * ? / 1e6 + output_tokens * ? / 1e6 +
			cache_create_tokens * ? / 1e6 + cache_read_tokens * ? / 1e6
			WHERE model = ?`, p[0], p[1], p[2], p[3], model); err != nil {
			return err
		}
	}
	// A session with events carries the authoritative per-request breakdown, and
	// its usage row is a rollup of them — including sessions whose requests span
	// several models, where the rollup's own model column names only one of them
	// and repricing by that name would charge the whole session at one model's
	// rate. Sum the events instead. Tokens are left alone; only cost moved.
	if err := execTx(tx, `UPDATE usage SET cost_usd = COALESCE((
			SELECT SUM(e.cost_usd) FROM usage_events e WHERE e.session_id = usage.session_id), 0)
		WHERE EXISTS (SELECT 1 FROM usage_events e WHERE e.session_id = usage.session_id)`); err != nil {
		return err
	}
	// What is left are sessions whose transcript only ever reported a total, so
	// the usage row is the original record and its single model is the right rate.
	models, err = distinctUsageModels(tx, "usage")
	if err != nil {
		return err
	}
	for _, model := range models {
		p, _ := lookupPricing(model)
		if err := execTx(tx, `UPDATE usage SET cost_usd =
			input_tokens * ? / 1e6 + output_tokens * ? / 1e6 +
			cache_create_tokens * ? / 1e6 + cache_read_tokens * ? / 1e6
			WHERE model = ? AND NOT EXISTS (
				SELECT 1 FROM usage_events e WHERE e.session_id = usage.session_id)`,
			p[0], p[1], p[2], p[3], model); err != nil {
			return err
		}
	}
	return nil
}

func distinctUsageModels(tx *sql.Tx, table string) ([]string, error) {
	// table is one of two literals chosen above, never caller input.
	rows, err := tx.Query(`SELECT DISTINCT model FROM ` + table + ` WHERE model <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var models []string
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	return models, rows.Err()
}
