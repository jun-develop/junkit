package scaffold

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
)

// migrationSeqPattern matches "NNNNNN_name.up.sql" and captures the sequence number.
var migrationSeqPattern = regexp.MustCompile(`^(\d{6})_.*\.up\.sql$`)

// NextMigrationSeq scans dir for "NNNNNN_name.up.sql" files and returns the next 6-digit
// sequence number.
//
// Usage:
//
//	seq, err := scaffold.NextMigrationSeq("internal/database/primary/migrations")
func NextMigrationSeq(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "000001", nil
		}

		return "", fmt.Errorf("scaffold: read %s: %w", dir, err)
	}

	max := 0
	for _, e := range entries {
		m := migrationSeqPattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}

		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}

		if n > max {
			max = n
		}
	}

	return fmt.Sprintf("%06d", max+1), nil
}
