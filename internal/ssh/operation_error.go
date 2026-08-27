package ssh

import "fmt"

type OperationError struct {
	Phase       string
	Retryable   bool
	UserMessage string
	Err         error
}

func (e OperationError) Error() string {
	switch {
	case e.UserMessage != "" && e.Err != nil:
		return fmt.Sprintf("%s: %v", e.UserMessage, e.Err)
	case e.UserMessage != "":
		return e.UserMessage
	case e.Err != nil:
		return e.Err.Error()
	default:
		return "ssh operation failed"
	}
}

func (e OperationError) Unwrap() error {
	return e.Err
}
