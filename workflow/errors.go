package workflow

import "errors"

var (
	// ErrPortInUse identifies a requested browser port that cannot be used.
	ErrPortInUse = errors.New("port in use")
	// ErrContainerConflict identifies an unrelated container using a requested name.
	ErrContainerConflict = errors.New("container conflict")
	// ErrImageDownload identifies a failed application-image pull.
	ErrImageDownload = errors.New("image download failed")
)

type classifiedError struct {
	kind error
	err  error
}

func (e *classifiedError) Error() string { return e.err.Error() }
func (e *classifiedError) Unwrap() []error {
	return []error{e.kind, e.err}
}

func classifyError(kind, err error) error {
	if err == nil {
		return nil
	}
	return &classifiedError{kind: kind, err: err}
}
