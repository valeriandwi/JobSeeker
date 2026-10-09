package search

import (
	"context"
	"fmt"
)

// ListAllSourceIDs returns every source id in the catalog, regardless of
// enablement. Used to validate requests without inventing a source.
func (r *Repository) ListAllSourceIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.Pool.Query(ctx, `select id from job_sources order by id`)
	if err != nil {
		return nil, fmt.Errorf("listing source ids: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
