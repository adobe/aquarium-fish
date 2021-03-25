package util

type UnparsedJson string

func (r *UnparsedJson) MarshalJSON() ([]byte, error) {
	return []byte(*r), nil
}

func (r *UnparsedJson) UnmarshalJSON(b []byte) error {
	// Store json as string
	*r = UnparsedJson(b)
	return nil
}
