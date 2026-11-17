package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jun-develop/junkit/database"
)

// init registers classify with database.Classify, so importing this package is enough.
func init() {
	database.RegisterClassifier(classify)
}

// classify implements database.Classifier for pgx errors. Its detail may echo raw Postgres messages
// and is for internal logs only (see database.Classify).
func classify(err error) (database.ErrorType, string, bool) {
	if errors.Is(err, pgx.ErrNoRows) {
		return database.ErrorNotFound, "resource not found", true
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		errType, detail := classifyPgError(pgErr)
		return errType, detail, true
	}

	return database.ErrorNone, "", false
}

// classifyPgError maps a Postgres SQLSTATE code to an ErrorType. The P0001 and default details
// carry pgErr.Message verbatim, which is internal-only.
// See https://www.postgresql.org/docs/current/errcodes-appendix.html.
func classifyPgError(pgErr *pgconn.PgError) (database.ErrorType, string) {
	switch pgErr.Code {
	// raise_exception
	case "P0001":
		return database.ErrorBusiness, pgErr.Message
	// unique_violation
	case "23505":
		return database.ErrorUniqueViolation, fmt.Sprintf("duplicate value violates unique constraint (%s)", pgErr.ConstraintName)
	// foreign_key_violation
	case "23503":
		return database.ErrorForeignKey, fmt.Sprintf("foreign key constraint violation (%s)", pgErr.ConstraintName)
	// check_violation
	case "23514":
		return database.ErrorCheckViolation, fmt.Sprintf("check constraint violation (%s)", pgErr.ConstraintName)
	// invalid_password, insufficient_privilege
	case "28P01", "42501":
		return database.ErrorPermissionDenied, "permission denied"
	// connection_exception class
	case "08000", "08001", "08003", "08004", "08006", "08P01":
		return database.ErrorConnection, "database connection error"
	// invalid_text_representation, invalid_datetime_format, datetime_field_overflow
	case "22P02", "22007", "22008":
		return database.ErrorInvalidInput, "invalid input format"
	default:
		return database.ErrorUnknown, fmt.Sprintf("database error (%s): %s", pgErr.Code, pgErr.Message)
	}
}
