// Package fault provides stable, secret-free errors for CLI and MCP clients.
package fault

import "errors"

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func New(code, message string) error { return &Error{Code: code, Message: message} }

func Public(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: "LOCAL_ERROR", Message: "Échec local ; vérifier les chemins, permissions et fichiers de configuration."}
}
