package workflow

import (
	"errors"
	"fmt"
)

var (
	// ErrPortInUse identifies a requested browser port that cannot be used.
	ErrPortInUse = errors.New("port in use")
	// ErrContainerConflict identifies an unrelated container using a requested name.
	ErrContainerConflict = errors.New("container conflict")
	// ErrPortOwned identifies a port another saved Omnideck installation
	// uses. It also satisfies ErrPortInUse.
	ErrPortOwned = errors.New("port owned by another installation")
	// ErrInstanceNameTaken identifies a name or data volume that belongs to
	// another saved Omnideck installation. Unlike ErrContainerConflict, the
	// existing container must never be suggested for removal.
	ErrInstanceNameTaken = errors.New("instance name taken")
	// ErrImageDownload identifies a failed application-image pull.
	ErrImageDownload = errors.New("image download failed")
	// ErrMissingStorage identifies a saved instance whose expected data volume
	// is absent. Repair must not silently create an empty replacement.
	ErrMissingStorage = errors.New("saved storage missing")
	// ErrExternalURL identifies a container whose OMNIDECK_EXTERNAL_URL does
	// not match the instance's browser address after Omnideck configured it.
	ErrExternalURL = errors.New("external URL mismatch")
	// ErrContainerNotFound identifies a saved instance whose container is
	// missing, so it cannot be started until it is repaired.
	ErrContainerNotFound = errors.New("container not found")
)

type classifiedError struct {
	kind error
	err  error
}

func (e *classifiedError) Error() string { return e.err.Error() }
func (e *classifiedError) Unwrap() []error {
	return []error{e.kind, e.err}
}

// InstanceNameTakenError reports that name belongs to another saved Omnideck
// installation.
func InstanceNameTakenError(name string) error {
	return classifyError(ErrInstanceNameTaken, fmt.Errorf("another Omnideck installation already uses the name %q; choose a different --name", name))
}

// ContainerConflictError reports that a container Omnideck did not save
// already uses name.
func ContainerConflictError(name string) error {
	return classifyError(ErrContainerConflict, fmt.Errorf("another container already uses the name %q; choose a different --name", name))
}

// InstallationPortError reports that another saved Omnideck installation
// already uses port.
func InstallationPortError(port string) error {
	return classifyError(ErrPortInUse, classifyError(ErrPortOwned, fmt.Errorf("another Omnideck installation already uses port %s; choose a different --port", port)))
}

// PortInUseError reports that another app is listening on port.
func PortInUseError(port string) error {
	return classifyError(ErrPortInUse, fmt.Errorf("another app is already using port %s; choose a different --port", port))
}

func classifyError(kind, err error) error {
	if err == nil {
		return nil
	}
	return &classifiedError{kind: kind, err: err}
}
