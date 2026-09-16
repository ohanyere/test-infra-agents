package environment

import (
	"errors"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
)

func mapPgError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return identity.ErrConflict
		case "23503":
			return identity.ErrNotFound
		case "23514", "23502", "22P02":
			return identity.ErrValidation
		case "22P01", "22000":
			return identity.ErrValidation
		}
		if validationErrorCode(pgErr.Code) {
			return identity.ErrValidation
		}
	}
	var target validationError
	if errors.As(err, &target) {
		return identity.ErrValidation
	}
	return err
}

func validationErrorCode(code string) bool {
	// Class 23 integrity violations surface as validation errors at the API.
	return len(code) == 5 && code[0] == '2' && code[1] == '3'
}
