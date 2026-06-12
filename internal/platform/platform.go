package platform

// ManualStepsError reports that an operation needs elevation Hull could
// not obtain; Instructions tell the user exactly what to run.
type ManualStepsError struct {
	Instructions string
}

func (e *ManualStepsError) Error() string {
	return "manual steps required:\n" + e.Instructions
}
