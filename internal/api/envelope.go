package api

import (
	"encoding/json"
	"net/http"
)

type envelopeOK struct {
	OK   bool `json:"ok"`
	Data any  `json:"data"`
}

type envelopeErr struct {
	OK    bool `json:"ok"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details any    `json:"details,omitempty"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func OK(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, envelopeOK{OK: true, Data: data})
}

func Fail(w http.ResponseWriter, status int, code, message string, details any) {
	var e envelopeErr
	e.OK = false
	e.Error.Code = code
	e.Error.Message = message
	e.Error.Details = details
	writeJSON(w, status, e)
}
