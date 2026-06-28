package ingest

type HashConflictError struct {
	Message string
}

func (e HashConflictError) Error() string {
	return e.Message
}

type HashValidationError struct {
	Message string
}

func (e HashValidationError) Error() string {
	return e.Message
}

type StorageError struct {
	Message string
}

func (e StorageError) Error() string {
	return e.Message
}

type LedgerError struct {
	Message string
}

func (e LedgerError) Error() string {
	return e.Message
}
