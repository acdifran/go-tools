package clienterror

import (
	"errors"
)

type Error struct {
	err       error
	msg       string
	clientmsg string
}

func (e *Error) Error() string {
	return e.msg
}

func (e *Error) ClientMsg() string {
	return e.clientmsg
}

func NewClientError(msg string, clientmsg string) *Error {
	return &Error{msg: msg, clientmsg: clientmsg}
}

func Wrap(err error, clientMsg string) error {
	var cerr *Error
	if errors.As(err, &cerr) {
		return err
	}
	return &Error{err: err, msg: err.Error(), clientmsg: clientMsg}
}

func (err *Error) Unwrap() error {
	return err.err
}

func NewUnauthorizedError() *Error {
	return &Error{clientmsg: "You are not authorized to view this content", msg: "Unauthorized"}
}
