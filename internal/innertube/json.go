package innertube

import "encoding/json"

func MarshalRequest(v any) ([]byte, error) {
	return json.Marshal(v)
}
