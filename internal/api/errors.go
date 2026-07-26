package api

import (
	"errors"
	"net/http"

	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

func mapSupervisorErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, supervisor.ErrConflict), errors.Is(err, supervisor.ErrPrecondition):
		Fail(w, http.StatusConflict, "conflict", err.Error(), nil)
	case errors.Is(err, supervisor.ErrUnsupported):
		Fail(w, http.StatusUnprocessableEntity, "unsupported", err.Error(), nil)
	case errors.Is(err, supervisor.ErrInvalid):
		Fail(w, http.StatusBadRequest, "config_invalid", err.Error(), nil)
	case errors.Is(err, supervisor.ErrNotFound):
		Fail(w, http.StatusNotFound, "not_found", err.Error(), nil)
	default:
		Fail(w, http.StatusInternalServerError, "internal", err.Error(), nil)
	}
}
