package frontend

func convertParseErrors(errors []parseError) []error {
	if len(errors) == 0 {
		return nil
	}
	converted := make([]error, 0, len(errors))
	for _, err := range errors {
		converted = append(converted, err)
	}
	return converted
}
